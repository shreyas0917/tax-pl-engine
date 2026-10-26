package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"github.com/shreyas0917/taxpl/internal/blob"
	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/normalize"
	"github.com/shreyas0917/taxpl/internal/parser"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/store"
)

// Job states (spec Section 8).
const (
	StateUploaded    = "UPLOADED"
	StateParsing     = "PARSING"
	StateNormalized  = "NORMALIZED"
	StateAdjusted    = "ADJUSTED"
	StateMatched     = "MATCHED"
	StateComputed    = "COMPUTED"
	StateReportReady = "REPORT_READY"
	StateNeedsReview = "NEEDS_REVIEW"
	StateFailed      = "FAILED"
)

// Job kinds.
const (
	KindProcessUpload = "PROCESS_UPLOAD"
	KindRecompute     = "RECOMPUTE"
	KindReport        = "REPORT"
)

// MaxAttempts before a job is marked FAILED.
const MaxAttempts = 3

// Service wires the workers to their dependencies.
type Service struct {
	Pool   *pgxpool.Pool // user-scoped work (RLS); every transaction sets app.user_id
	Sys    *pgxpool.Pool // cross-user maintenance (purge); nil = Pool
	Blob   blob.Store
	Log    *slog.Logger
	River  *river.Client[pgx.Tx]
	RawTTL time.Duration
	// ReportWriters produce report bytes per format; registered by the report package.
	ReportWriters map[string]ReportWriter
}

// ReportWriter renders a report for one financial year.
type ReportWriter func(rep *pipeline.Report, fy string) ([]byte, string, error)

// Migrate applies River's own schema.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	_, err = m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

// NewInsertOnlyClient is used by the API to enqueue jobs without running workers.
func NewInsertOnlyClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{})
}

// NewWorkerClient builds the worker-side client with all workers registered.
func NewWorkerClient(pool *pgxpool.Pool, svc *Service, workerCount int, log *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &processUploadWorker{svc: svc})
	river.AddWorker(workers, &recomputeWorker{svc: svc})
	river.AddWorker(workers, &reportWorker{svc: svc})
	river.AddWorker(workers, &purgeWorker{svc: svc})
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) {
			return PurgeArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
		}, &river.PeriodicJobOpts{RunOnStart: true}),
	}
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: workerCount}},
		Workers:      workers,
		PeriodicJobs: periodic,
		Logger:       log,
		// Spec: 3 attempts with exponential backoff (River's default policy is attempt^4 seconds).
		MaxAttempts: MaxAttempts,
	})
	if err != nil {
		return nil, err
	}
	svc.River = c
	return c, nil
}

// --- job args -----------------------------------------------------------------------------

// ProcessUploadArgs parses one upload into trades.
type ProcessUploadArgs struct {
	UserID   string `json:"user_id"`
	UploadID string `json:"upload_id"`
	JobID    string `json:"job_id"`
}

// Kind implements river.JobArgs.
func (ProcessUploadArgs) Kind() string { return "process_upload" }

// RecomputeArgs rebuilds one user's derived tables.
type RecomputeArgs struct {
	UserID string `json:"user_id"`
}

// Kind implements river.JobArgs.
func (RecomputeArgs) Kind() string { return "recompute" }

// ReportArgs renders one report.
type ReportArgs struct {
	UserID   string `json:"user_id"`
	JobID    string `json:"job_id"`
	ReportID string `json:"report_id"`
	FY       string `json:"fy"`
	Format   string `json:"format"`
}

// Kind implements river.JobArgs.
func (ReportArgs) Kind() string { return "report" }

// PurgeArgs deletes raw files older than the TTL.
type PurgeArgs struct{}

// Kind implements river.JobArgs.
func (PurgeArgs) Kind() string { return "purge_raw_files" }

// EnqueueRecompute queues a recompute for the user inside tx. Recomputes are not
// de-duplicated: River's unique jobs would collapse a request that arrives while another
// recompute is already running, and that run cannot have seen the newer trades. Duplicate
// recomputes are harmless because each one is idempotent and they are serialised by the
// per-user advisory lock.
func EnqueueRecompute(ctx context.Context, client *river.Client[pgx.Tx], tx pgx.Tx, userID string) error {
	_, err := client.InsertTx(ctx, tx, RecomputeArgs{UserID: userID}, &river.InsertOpts{MaxAttempts: MaxAttempts, Queue: Queue})
	return err
}

// Queue is the River queue jobs are inserted into. Tests point it at a queue no worker
// serves so a live development worker cannot interfere with their assertions.
var Queue = river.QueueDefault

// --- state transitions ----------------------------------------------------------------------

// Transition records a state change: jobs.state, a job_events row (SSE) and an audit row.
func Transition(ctx context.Context, q *store.Queries, jobID, userID, state, message string, detail any, jobErr error) error {
	var errJSON []byte
	if jobErr != nil {
		errJSON, _ = json.Marshal(map[string]string{"message": jobErr.Error()})
	}
	before, err := q.GetJobAny(ctx, jobID)
	if err != nil {
		return err
	}
	after, err := q.SetJobState(ctx, store.SetJobStateParams{ID: jobID, State: state, Error: errJSON, RulesVersion: "", AttemptInc: 0})
	if err != nil {
		return err
	}
	var detailJSON []byte
	if detail != nil {
		detailJSON, _ = json.Marshal(detail)
	}
	if _, err := q.InsertJobEvent(ctx, store.InsertJobEventParams{JobID: jobID, UserID: userID, State: state, Message: message, Detail: detailJSON}); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]string{"state": before.State})
	a, _ := json.Marshal(map[string]string{"state": after.State})
	return q.InsertAudit(ctx, store.InsertAuditParams{UserID: &userID, Entity: "job", EntityID: jobID, Action: "state:" + state, Before: b, After: a})
}

func (s *Service) fail(ctx context.Context, jobID, userID string, err error) {
	s.Log.Error("job failed", "job_id", jobID, "user_id", userID, "err", err)
	_ = store.WithUserTx(ctx, s.Pool, userID, func(_ pgx.Tx, q *store.Queries) error {
		return Transition(ctx, q, jobID, userID, StateFailed, friendly(err), nil, err)
	})
}

func friendly(err error) string {
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300] + "..."
	}
	return "Processing failed: " + msg
}

func lastAttempt(job *rivertype.JobRow) bool { return job.Attempt >= job.MaxAttempts }

// --- process upload -----------------------------------------------------------------------

type processUploadWorker struct {
	river.WorkerDefaults[ProcessUploadArgs]
	svc *Service
}

func (w *processUploadWorker) Work(ctx context.Context, job *river.Job[ProcessUploadArgs]) error {
	err := w.svc.ProcessUpload(ctx, job.Args)
	if err != nil && lastAttempt(job.JobRow) {
		w.svc.fail(ctx, job.Args.JobID, job.Args.UserID, err)
	}
	return err
}

// ProcessUpload parses and normalises one upload, stores the trades idempotently and queues
// a recompute, all in one transaction.
func (s *Service) ProcessUpload(ctx context.Context, a ProcessUploadArgs) error {
	var up store.Upload
	var instruments map[string]domain.Instrument
	var symbolToISIN map[string]string
	if err := store.WithUserTx(ctx, s.Pool, a.UserID, func(_ pgx.Tx, q *store.Queries) error {
		var err error
		if up, err = q.GetUpload(ctx, store.GetUploadParams{ID: a.UploadID, UserID: a.UserID}); err != nil {
			return fmt.Errorf("load upload: %w", err)
		}
		if instruments, symbolToISIN, err = s.referenceData(ctx, q, a.UserID); err != nil {
			return err
		}
		return Transition(ctx, q, a.JobID, a.UserID, StateParsing, "Reading "+up.Filename, nil, nil)
	}); err != nil {
		return err
	}
	rc, err := s.Blob.Get(ctx, up.BlobKey)
	if err != nil {
		return fmt.Errorf("read raw file (it may have been purged after %s): %w", s.RawTTL, err)
	}
	data, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return err
	}

	var p parser.Parser
	brokerName := up.Broker
	if brokerName != "" {
		pp, ok := parser.Get(brokerName)
		if !ok {
			return fmt.Errorf("unknown broker %q", brokerName)
		}
		p = pp
	} else {
		pp, det, ok := parser.Best(data)
		if !ok {
			return fmt.Errorf("could not detect the broker format of %s; re-upload and choose the broker", up.Filename)
		}
		p, brokerName = pp, det.Broker
	}
	rows, issues, err := p.Parse(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("parse %s: %w", up.Filename, err)
	}

	n := normalize.Normalize(normalize.Input{AccountID: up.AccountID, Broker: brokerName, SourceFile: up.Filename, Rows: rows, Instruments: instruments, SymbolToISIN: symbolToISIN})

	return store.WithUserTx(ctx, s.Pool, a.UserID, func(tx pgx.Tx, q *store.Queries) error {
		inserted := 0
		for _, t := range n.Trades {
			rows, err := q.InsertTrade(ctx, TradeInsertParams(a.UserID, &a.UploadID, t))
			if err != nil {
				return fmt.Errorf("insert trade row %d: %w", t.SourceRow, err)
			}
			inserted += int(rows)
		}
		var keys []string
		for _, is := range issues {
			key := domain.SourceRowHash("issue", a.UploadID, fmt.Sprint(is.Line))
			keys = append(keys, key)
			payload, _ := json.Marshal(map[string]any{"file": up.Filename, "line": is.Line, "reason": is.Reason})
			if err := q.UpsertReviewItem(ctx, store.UpsertReviewItemParams{UserID: a.UserID, JobID: &a.JobID, ReviewKey: key, Kind: string(domain.ReviewParseIssue), AccountID: &up.AccountID,
				Message: fmt.Sprintf("%s line %d could not be read: %s", up.Filename, is.Line, is.Reason), Payload: payload}); err != nil {
				return err
			}
		}
		for _, r := range n.Review {
			keys = append(keys, r.ID)
			payload, _ := json.Marshal(r.Payload)
			if err := q.UpsertReviewItem(ctx, store.UpsertReviewItemParams{UserID: a.UserID, JobID: &a.JobID, ReviewKey: r.ID, Kind: string(r.Kind), AccountID: &up.AccountID,
				Isin: r.ISIN, Symbol: r.Symbol, TradeKey: r.TradeID, Message: r.Message, Payload: payload}); err != nil {
				return err
			}
		}
		if err := q.SetUploadStatus(ctx, store.SetUploadStatusParams{ID: a.UploadID, UserID: a.UserID, Status: StateNormalized, Column4: brokerName}); err != nil {
			return err
		}
		detail := map[string]any{"broker": brokerName, "rows": len(rows) + len(issues), "trades": len(n.Trades), "new_trades": inserted, "duplicates": n.Duplicates + len(n.Trades) - inserted, "issues": len(issues), "review_keys": keys}
		if err := Transition(ctx, q, a.JobID, a.UserID, StateNormalized, fmt.Sprintf("Parsed %d trades (%d new, %d issues) from %s", len(n.Trades), inserted, len(issues), up.Filename), detail, nil); err != nil {
			return err
		}
		return EnqueueRecompute(ctx, s.River, tx, a.UserID)
	})
}

func (s *Service) referenceData(ctx context.Context, q *store.Queries, userID string) (map[string]domain.Instrument, map[string]string, error) {
	instruments := map[string]domain.Instrument{}
	symbolToISIN := map[string]string{}
	refs, err := q.ListInstruments(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range refs {
		inst := InstrumentFromRow(r)
		instruments[inst.ISIN] = inst
		symbolToISIN[strings.ToUpper(inst.Symbol)] = inst.ISIN
	}
	ovs, err := q.ListInstrumentOverrides(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	for _, o := range ovs {
		inst := instruments[o.Isin]
		inst.ISIN = o.Isin
		if o.Symbol != "" {
			inst.Symbol = o.Symbol
			symbolToISIN[strings.ToUpper(o.Symbol)] = o.Isin
		}
		if o.Fmv20180131.Valid {
			inst.FMV2018 = decPtr(o.Fmv20180131)
		}
		if inst.Segment == "" {
			inst.Segment = domain.SegEquity
		}
		instruments[o.Isin] = inst
	}
	return instruments, symbolToISIN, nil
}

// --- recompute ----------------------------------------------------------------------------

type recomputeWorker struct {
	river.WorkerDefaults[RecomputeArgs]
	svc *Service
}

func (w *recomputeWorker) Work(ctx context.Context, job *river.Job[RecomputeArgs]) error {
	err := w.svc.Recompute(ctx, job.Args.UserID, "")
	if err != nil && lastAttempt(job.JobRow) {
		// Mark every job waiting on this recompute as failed.
		var waiting []store.Job
		_ = store.WithUserTx(ctx, w.svc.Pool, job.Args.UserID, func(_ pgx.Tx, q *store.Queries) error {
			for _, st := range []string{StateNormalized, StateAdjusted, StateMatched} {
				js, _ := q.ListJobsInState(ctx, store.ListJobsInStateParams{UserID: job.Args.UserID, State: st})
				waiting = append(waiting, js...)
			}
			return nil
		})
		for _, j := range waiting {
			w.svc.fail(ctx, j.ID, job.Args.UserID, err)
		}
	}
	return err
}

// BuildRequest loads everything the pure pipeline needs for a user.
func BuildRequest(ctx context.Context, q *store.Queries, userID string) (pipeline.Request, error) {
	rows, err := q.ListTradesForUser(ctx, userID)
	if err != nil {
		return pipeline.Request{}, err
	}
	instruments, symbolToISIN, err := (&Service{}).referenceData(ctx, q, userID)
	if err != nil {
		return pipeline.Request{}, err
	}
	cas, err := q.ListCorporateActions(ctx)
	if err != nil {
		return pipeline.Request{}, err
	}
	ols, err := q.ListOpeningLots(ctx, userID)
	if err != nil {
		return pipeline.Request{}, err
	}
	req := pipeline.Request{Instruments: instruments, SymbolToISIN: symbolToISIN, AsOf: domain.DateOf(time.Now().UTC())}
	for _, r := range rows {
		req.ExtraTrades = append(req.ExtraTrades, TradeFromRow(r))
	}
	for _, c := range cas {
		req.CorporateActions = append(req.CorporateActions, CorporateActionFromRow(c))
	}
	for _, o := range ols {
		req.OpeningLots = append(req.OpeningLots, OpeningLotFromRow(o))
	}
	return req, nil
}

// Recompute rebuilds lots, gains and review items for a user inside one transaction guarded
// by a per-user advisory lock. jobID, when set, is the job to report progress on; otherwise
// every job of the user waiting in NORMALIZED state is advanced.
func (s *Service) Recompute(ctx context.Context, userID, jobID string) error {
	return store.WithUserTx(ctx, s.Pool, userID, func(tx pgx.Tx, q *store.Queries) error {
		if err := store.LockUser(ctx, tx, userID); err != nil {
			return err
		}
		var jobIDs []string
		if jobID != "" {
			jobIDs = []string{jobID}
		} else {
			waiting, err := q.ListJobsInState(ctx, store.ListJobsInStateParams{UserID: userID, State: StateNormalized})
			if err != nil {
				return err
			}
			for _, j := range waiting {
				jobIDs = append(jobIDs, j.ID)
			}
		}
		advance := func(state, msg string, detail any) error {
			for _, id := range jobIDs {
				if err := Transition(ctx, q, id, userID, state, msg, detail, nil); err != nil {
					return err
				}
			}
			return nil
		}
		req, err := BuildRequest(ctx, q, userID)
		if err != nil {
			return err
		}
		if err := advance(StateAdjusted, "Applying corporate actions", nil); err != nil {
			return err
		}
		rep, err := pipeline.Run(req)
		if err != nil {
			return fmt.Errorf("compute: %w", err)
		}
		// Explanations are stored verbatim; show account nicknames instead of UUIDs in them.
		if accounts, err := q.ListAccounts(ctx, userID); err == nil {
			repl := make([]string, 0, 2*len(accounts))
			for _, a := range accounts {
				repl = append(repl, a.ID, a.Nickname+" ("+a.Broker+")")
			}
			r := strings.NewReplacer(repl...)
			for i := range rep.Gains {
				for j, st := range rep.Gains[i].Steps {
					rep.Gains[i].Steps[j] = r.Replace(st)
				}
			}
		}
		if err := advance(StateMatched, fmt.Sprintf("Matched %d sales FIFO across %d lots", len(rep.Matches), len(rep.Lots)), nil); err != nil {
			return err
		}

		// Rebuild derived tables.
		if err := q.DeleteDerivedForUser(ctx, userID); err != nil {
			return err
		}
		if err := q.DeleteLotsForUser(ctx, userID); err != nil {
			return err
		}
		for _, l := range rep.Lots {
			adj, _ := json.Marshal(l.Adjustments)
			if err := q.InsertLot(ctx, store.InsertLotParams{UserID: userID, AccountID: l.AccountID, LotKey: l.ID, Isin: l.ISIN, Symbol: l.Symbol, OpenTradeKey: l.OpenTradeID,
				Origin: string(l.Origin), QuantityOriginal: l.QuantityOriginal, QuantityOpen: l.QuantityOpen, CostOriginal: l.CostOriginal, CostOpen: l.CostOpen,
				AcquisitionDate: fromDate(l.AcquisitionDate), GrandfatheredFmv: nullDec(l.GrandfatheredFMV), Adjustments: adj}); err != nil {
				return fmt.Errorf("insert lot: %w", err)
			}
		}
		for _, g := range rep.Gains {
			explain, _ := json.Marshal(g)
			if err := q.InsertGain(ctx, store.InsertGainParams{UserID: userID, AccountID: g.AccountID, GainKey: g.ID, Fy: g.FY, Category: string(g.Category), Isin: g.ISIN, Symbol: g.Symbol,
				ContractKey: g.ContractKey, SaleDate: fromDate(g.SaleDate), AcquisitionDate: fromDatePtr(g.AcquisitionDate), Quantity: g.Quantity, SaleValue: g.SaleValue,
				ActualCost: g.ActualCost, CostOfAcquisition: g.CostOfAcquisition, ChargesDeductible: g.ChargesDeductible, Gain: g.Gain, HoldingDays: int32(g.HoldingDays),
				RatePeriod: g.RatePeriod, BuybackRegime: g.BuybackRegime, BuybackProceeds: g.BuybackProceeds, RulesVersion: g.RulesVersion, Explain: explain}); err != nil {
				return fmt.Errorf("insert gain: %w", err)
			}
		}
		keys := make([]string, 0, len(rep.Review))
		for _, r := range rep.Review {
			if r.Kind == domain.ReviewParseIssue && r.AccountID == "" {
				continue // year-without-rules notices are informational
			}
			keys = append(keys, r.ID)
			payload, _ := json.Marshal(r.Payload)
			var acct *string
			if r.AccountID != "" {
				acct = strPtr(r.AccountID)
			}
			var jid *string
			if len(jobIDs) > 0 {
				jid = &jobIDs[0]
			}
			if err := q.UpsertReviewItem(ctx, store.UpsertReviewItemParams{UserID: userID, JobID: jid, ReviewKey: r.ID, Kind: string(r.Kind), AccountID: acct,
				Isin: r.ISIN, Symbol: r.Symbol, TradeKey: r.TradeID, Message: r.Message, Payload: payload}); err != nil {
				return err
			}
		}
		// Parse-issue items are owned by the upload job and are not re-raised by the engine, so keep them.
		open, err := q.ListReviewItems(ctx, store.ListReviewItemsParams{UserID: userID, Status: strPtr("open")})
		if err != nil {
			return err
		}
		for _, it := range open {
			if it.Kind == string(domain.ReviewParseIssue) {
				keys = append(keys, it.ReviewKey)
			}
		}
		if _, err := q.CloseStaleReviewItems(ctx, store.CloseStaleReviewItemsParams{UserID: userID, Column2: keys}); err != nil {
			return err
		}
		b, err := jsonreport.Marshal(rep)
		if err != nil {
			return err
		}
		rv, _ := json.Marshal(rep.RulesVersions)
		var firstJob *string
		if len(jobIDs) > 0 {
			firstJob = &jobIDs[0]
		}
		comp, err := q.InsertComputation(ctx, store.InsertComputationParams{UserID: userID, JobID: firstJob, RulesVersions: rv, Checksum: jsonreport.Checksum(b), TradeCount: int32(len(rep.Trades))})
		if err != nil {
			return err
		}
		openCount, err := q.CountOpenReviewItems(ctx, userID)
		if err != nil {
			return err
		}
		detail := map[string]any{"gains": len(rep.Gains), "lots": len(rep.Lots), "review_open": openCount, "checksum": comp.Checksum, "rules_versions": rep.RulesVersions}
		if err := advance(StateComputed, fmt.Sprintf("Classified %d gain lines", len(rep.Gains)), detail); err != nil {
			return err
		}
		final, msg := StateReportReady, "Done. Your summary is ready."
		if openCount > 0 {
			final, msg = StateNeedsReview, fmt.Sprintf("Done, with %d item(s) that need your input.", openCount)
		}
		return advance(final, msg, detail)
	})
}

// --- report -------------------------------------------------------------------------------

type reportWorker struct {
	river.WorkerDefaults[ReportArgs]
	svc *Service
}

func (w *reportWorker) Work(ctx context.Context, job *river.Job[ReportArgs]) error {
	err := w.svc.GenerateReport(ctx, job.Args)
	if err != nil && lastAttempt(job.JobRow) {
		w.svc.fail(ctx, job.Args.JobID, job.Args.UserID, err)
	}
	return err
}

// GenerateReport renders the report for a year from a fresh pipeline run (so it always
// reflects the stored trades exactly) and records its checksum.
func (s *Service) GenerateReport(ctx context.Context, a ReportArgs) error {
	writer, ok := s.ReportWriters[a.Format]
	if !ok {
		return fmt.Errorf("report format %q is not supported", a.Format)
	}
	var req pipeline.Request
	var accounts []store.DematAccount
	if err := store.WithUserTx(ctx, s.Pool, a.UserID, func(_ pgx.Tx, q *store.Queries) error {
		var err error
		if req, err = BuildRequest(ctx, q, a.UserID); err != nil {
			return err
		}
		if accounts, err = q.ListAccounts(ctx, a.UserID); err != nil {
			return err
		}
		return Transition(ctx, q, a.JobID, a.UserID, StateComputed, "Generating "+strings.ToUpper(a.Format)+" report for "+a.FY, nil, nil)
	}); err != nil {
		return err
	}
	req.FY = a.FY
	// Account IDs are UUIDs in the database; reports need the nicknames.
	names := map[string]string{}
	for _, acc := range accounts {
		names[acc.ID] = acc.Nickname + " (" + acc.Broker + ")"
	}
	for i := range req.ExtraTrades {
		if n, ok := names[req.ExtraTrades[i].AccountID]; ok {
			req.ExtraTrades[i].AccountID = n
		}
	}
	for i := range req.OpeningLots {
		if n, ok := names[req.OpeningLots[i].AccountID]; ok {
			req.OpeningLots[i].AccountID = n
		}
	}
	rep, err := pipeline.Run(req)
	if err != nil {
		return err
	}
	data, contentType, err := writer(rep, a.FY)
	if err != nil {
		return fmt.Errorf("render %s: %w", a.Format, err)
	}
	key := fmt.Sprintf("reports/%s/%s.%s", a.UserID, a.ReportID, a.Format)
	if err := s.Blob.Put(ctx, key, bytes.NewReader(data), contentType); err != nil {
		return err
	}
	rulesVersion := rep.RulesVersions[a.FY]
	return store.WithUserTx(ctx, s.Pool, a.UserID, func(_ pgx.Tx, q *store.Queries) error {
		rep, err := q.CreateReport(ctx, store.CreateReportParams{UserID: a.UserID, JobID: &a.JobID, Fy: a.FY, Format: a.Format, BlobKey: key, Checksum: jsonreport.Checksum(data), RulesVersion: rulesVersion, SizeBytes: int64(len(data))})
		if err != nil {
			return err
		}
		return Transition(ctx, q, a.JobID, a.UserID, StateReportReady, "Report ready", map[string]any{"report_id": rep.ID, "checksum": rep.Checksum, "format": a.Format, "fy": a.FY}, nil)
	})
}

// --- purge --------------------------------------------------------------------------------

type purgeWorker struct {
	river.WorkerDefaults[PurgeArgs]
	svc *Service
}

func (w *purgeWorker) Work(ctx context.Context, _ *river.Job[PurgeArgs]) error {
	return w.svc.PurgeRawFiles(ctx, time.Now())
}

// PurgeRawFiles deletes raw uploads older than the TTL (spec Section 13). Parsed trades stay.
func (s *Service) PurgeRawFiles(ctx context.Context, now time.Time) error {
	sys := s.Sys
	if sys == nil {
		sys = s.Pool
	}
	q := store.New(sys)
	ups, err := q.ListUploadsToPurge(ctx, now.Add(-s.RawTTL))
	if err != nil {
		return err
	}
	for _, up := range ups {
		if err := s.Blob.Delete(ctx, up.BlobKey); err != nil && !errors.Is(err, blob.ErrNotFound) {
			return err
		}
		if err := q.MarkUploadPurged(ctx, up.ID); err != nil {
			return err
		}
		_ = q.InsertAudit(ctx, store.InsertAuditParams{UserID: &up.UserID, Entity: "upload", EntityID: up.ID, Action: "purge_raw_file"})
	}
	if len(ups) > 0 {
		s.Log.Info("purged raw files", "count", len(ups))
	}
	return nil
}

// DeleteUserData removes every blob and row for a user (DELETE /me).
func (s *Service) DeleteUserData(ctx context.Context, userID string) error {
	var keys []string
	if err := store.WithUserTx(ctx, s.Pool, userID, func(_ pgx.Tx, q *store.Queries) error {
		ups, err := q.ListUploadBlobKeys(ctx, userID)
		if err != nil {
			return err
		}
		for _, u := range ups {
			keys = append(keys, u.BlobKey)
		}
		reps, err := q.ListReportBlobKeys(ctx, userID)
		if err != nil {
			return err
		}
		for _, r := range reps {
			keys = append(keys, r.BlobKey)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, k := range keys {
		if err := s.Blob.Delete(ctx, k); err != nil && !errors.Is(err, blob.ErrNotFound) {
			return err
		}
	}
	return store.WithUserTx(ctx, s.Pool, userID, func(_ pgx.Tx, q *store.Queries) error {
		if err := q.InsertAudit(ctx, store.InsertAuditParams{UserID: &userID, Entity: "user", EntityID: userID, Action: "delete_all_data"}); err != nil {
			return err
		}
		return q.DeleteUser(ctx, userID) // cascades to every user-owned table
	})
}
