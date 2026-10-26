package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/shreyas0917/taxpl/internal/jobs"
	"github.com/shreyas0917/taxpl/internal/parser"
	"github.com/shreyas0917/taxpl/internal/store"
)

type uploadView struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	Broker     string    `json:"broker"`
	Filename   string    `json:"filename"`
	SHA256     string    `json:"sha256"`
	SizeBytes  int64     `json:"size_bytes"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	Duplicate  bool      `json:"duplicate"`
	JobID      string    `json:"job_id,omitempty"`
	Confidence float64   `json:"detect_confidence,omitempty"`
}

// upload accepts multipart/form-data with fields file, account_id and optional broker.
// The same file (by SHA-256) uploaded twice returns the existing upload (spec Section 9).
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes+1<<16)
	if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
		if strings.Contains(err.Error(), "too large") {
			writeProblem(w, r, tooLarge(fmt.Sprintf("file must be at most %d MB", s.cfg.MaxUploadBytes>>20)))
			return
		}
		writeProblem(w, r, badRequest("expected multipart/form-data with a file field: "+err.Error()))
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, r, validation(map[string]string{"file": "a file is required"}))
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, s.cfg.MaxUploadBytes+1))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if int64(len(data)) > s.cfg.MaxUploadBytes {
		writeProblem(w, r, tooLarge(fmt.Sprintf("file must be at most %d MB", s.cfg.MaxUploadBytes>>20)))
		return
	}
	if len(data) == 0 {
		writeProblem(w, r, validation(map[string]string{"file": "file is empty"}))
		return
	}
	// Content sniffing: accept text (CSV) and the XLSX zip container only.
	ct := http.DetectContentType(data)
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	isZip := bytes.HasPrefix(data, []byte("PK\x03\x04"))
	acceptable := strings.HasPrefix(ct, "text/") || (isZip && ext == ".xlsx")
	if !acceptable {
		writeProblem(w, r, validation(map[string]string{"file": "only CSV or XLSX tradebooks are accepted (detected " + ct + ")"}))
		return
	}
	accountID := strings.TrimSpace(r.FormValue("account_id"))
	broker := strings.ToLower(strings.TrimSpace(r.FormValue("broker")))
	if accountID == "" {
		writeProblem(w, r, validation(map[string]string{"account_id": "required"}))
		return
	}
	if broker != "" {
		if _, ok := parser.Get(broker); !ok {
			writeProblem(w, r, validation(map[string]string{"broker": "unknown broker; known: " + strings.Join(parser.Brokers(), ", ")}))
			return
		}
	}
	uid := UserID(r.Context())
	if _, err := s.q.GetAccount(r.Context(), store.GetAccountParams{ID: accountID, UserID: uid}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProblem(w, r, validation(map[string]string{"account_id": "no such account"}))
			return
		}
		writeProblem(w, r, err)
		return
	}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	// Idempotent: existing upload with the same content is returned as-is.
	if existing, err := s.q.GetUploadBySHA(r.Context(), store.GetUploadBySHAParams{UserID: uid, Sha256: sha}); err == nil {
		v := uploadViewOf(existing)
		v.Duplicate = true
		if js, err := s.q.ListJobsForUpload(r.Context(), &existing.ID); err == nil && len(js) > 0 {
			v.JobID = js[len(js)-1].ID
			v.Status = js[len(js)-1].State
		}
		writeJSON(w, 200, v)
		return
	}

	var confidence float64
	if broker == "" {
		if !isZip {
			if _, det, ok := parser.Best(data); ok {
				broker, confidence = det.Broker, det.Confidence
			}
		}
	} else {
		confidence = 1
	}
	if broker == "" {
		writeProblem(w, r, validation(map[string]string{"broker": "could not detect the broker from the file; please choose it"}))
		return
	}

	key := fmt.Sprintf("raw/%s/%s%s", uid, sha[:32], ext)
	if err := s.blob.Put(r.Context(), key, bytes.NewReader(data), ct); err != nil {
		writeProblem(w, r, err)
		return
	}
	var out uploadView
	err = store.WithTx(r.Context(), s.pool, func(tx pgx.Tx, q *store.Queries) error {
		up, err := q.CreateUpload(r.Context(), store.CreateUploadParams{UserID: uid, AccountID: accountID, Broker: broker, Filename: filepath.Base(hdr.Filename),
			Sha256: sha, SizeBytes: int64(len(data)), BlobKey: key})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) { // lost a race with an identical upload
				existing, err := q.GetUploadBySHA(r.Context(), store.GetUploadBySHAParams{UserID: uid, Sha256: sha})
				if err != nil {
					return err
				}
				out = uploadViewOf(existing)
				out.Duplicate = true
				return nil
			}
			return err
		}
		payload, _ := json.Marshal(map[string]string{"filename": up.Filename})
		job, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, UploadID: &up.ID, Kind: jobs.KindProcessUpload, State: jobs.StateUploaded, Payload: payload})
		if err != nil {
			return err
		}
		if _, err := q.InsertJobEvent(r.Context(), store.InsertJobEventParams{JobID: job.ID, UserID: uid, State: jobs.StateUploaded, Message: "Uploaded " + up.Filename}); err != nil {
			return err
		}
		if err := q.InsertAudit(r.Context(), store.InsertAuditParams{UserID: &uid, Entity: "upload", EntityID: up.ID, Action: "create", After: payload}); err != nil {
			return err
		}
		if _, err := s.jobs.River.InsertTx(r.Context(), tx, jobs.ProcessUploadArgs{UserID: uid, UploadID: up.ID, JobID: job.ID}, &river.InsertOpts{MaxAttempts: jobs.MaxAttempts}); err != nil {
			return err
		}
		out = uploadViewOf(up)
		out.JobID = job.ID
		out.Confidence = confidence
		return nil
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	status := 201
	if out.Duplicate {
		status = 200
	}
	writeJSON(w, status, out)
}

func uploadViewOf(u store.Upload) uploadView {
	return uploadView{ID: u.ID, AccountID: u.AccountID, Broker: u.Broker, Filename: u.Filename, SHA256: u.Sha256, SizeBytes: u.SizeBytes, Status: u.Status, CreatedAt: u.CreatedAt}
}

func (s *Server) listUploads(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListUploads(r.Context(), store.ListUploadsParams{UserID: UserID(r.Context()), Limit: 100})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	out := make([]uploadView, 0, len(rows))
	for _, u := range rows {
		v := uploadViewOf(u)
		if js, err := s.q.ListJobsForUpload(r.Context(), &u.ID); err == nil && len(js) > 0 {
			v.JobID = js[len(js)-1].ID
			v.Status = js[len(js)-1].State
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

// enqueueRecompute creates a RECOMPUTE job row and queues it. The job ID is stored on the
// request header X-Recompute-Job for the handler to echo in its response.
func (s *Server) enqueueRecompute(r *http.Request, uid string) error {
	return store.WithTx(r.Context(), s.pool, func(tx pgx.Tx, q *store.Queries) error {
		job, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, Kind: jobs.KindRecompute, State: jobs.StateNormalized, Payload: []byte("{}")})
		if err != nil {
			return err
		}
		if _, err := q.InsertJobEvent(r.Context(), store.InsertJobEventParams{JobID: job.ID, UserID: uid, State: jobs.StateNormalized, Message: "Recompute requested"}); err != nil {
			return err
		}
		r.Header.Set("X-Recompute-Job", job.ID)
		return jobs.EnqueueRecompute(r.Context(), s.jobs.River, tx, uid)
	})
}
