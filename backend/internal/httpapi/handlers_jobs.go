package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/jobs"
	"github.com/shreyas0917/taxpl/internal/store"
)

type jobView struct {
	ID           string          `json:"id"`
	UploadID     *string         `json:"upload_id"`
	Kind         string          `json:"kind"`
	State        string          `json:"state"`
	RulesVersion string          `json:"rules_version"`
	Attempt      int32           `json:"attempt"`
	Error        json.RawMessage `json:"error,omitempty"`
	StartedAt    *time.Time      `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Terminal     bool            `json:"terminal"`
}

func jobViewOf(j store.Job) jobView {
	v := jobView{ID: j.ID, UploadID: j.UploadID, Kind: j.Kind, State: j.State, RulesVersion: j.RulesVersion, Attempt: j.Attempt, StartedAt: j.StartedAt,
		FinishedAt: j.FinishedAt, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, Terminal: isTerminal(j.State)}
	if len(j.Error) > 0 {
		v.Error = j.Error
	}
	return v
}

func isTerminal(state string) bool {
	switch state {
	case jobs.StateReportReady, jobs.StateNeedsReview, jobs.StateFailed:
		return true
	}
	return false
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListRecentJobs(r.Context(), store.ListRecentJobsParams{UserID: UserID(r.Context()), Limit: 50})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	out := make([]jobView, 0, len(rows))
	for _, j := range rows {
		out = append(out, jobViewOf(j))
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.q.GetJob(r.Context(), store.GetJobParams{ID: chi.URLParam(r, "id"), UserID: UserID(r.Context())})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	events, _ := s.q.ListJobEvents(r.Context(), store.ListJobEventsParams{JobID: j.ID, ID: 0})
	writeJSON(w, 200, map[string]any{"job": jobViewOf(j), "events": eventViews(events)})
}

type eventView struct {
	ID      int64           `json:"id"`
	State   string          `json:"state"`
	Message string          `json:"message"`
	Detail  json.RawMessage `json:"detail,omitempty"`
	At      time.Time       `json:"at"`
}

func eventViews(es []store.JobEvent) []eventView {
	out := make([]eventView, 0, len(es))
	for _, e := range es {
		v := eventView{ID: e.ID, State: e.State, Message: e.Message, At: e.At}
		if len(e.Detail) > 0 {
			v.Detail = e.Detail
		}
		out = append(out, v)
	}
	return out
}

// jobEvents streams job_events rows as Server-Sent Events, resuming from Last-Event-ID.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	j, err := s.q.GetJob(r.Context(), store.GetJobParams{ID: chi.URLParam(r, "id"), UserID: UserID(r.Context())})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, r, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	flusher.Flush()

	var last int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		last, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("last_event_id"); v != "" {
		last, _ = strconv.ParseInt(v, 10, 64)
	}
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(10 * time.Minute)
	for {
		events, err := s.q.ListJobEvents(r.Context(), store.ListJobEventsParams{JobID: j.ID, ID: last})
		if err != nil {
			return
		}
		for _, e := range events {
			b, _ := json.Marshal(eventView{ID: e.ID, State: e.State, Message: e.Message, Detail: e.Detail, At: e.At})
			_, _ = fmt.Fprintf(w, "id: %d\nevent: state\ndata: %s\n\n", e.ID, b)
			last = e.ID
			if isTerminal(e.State) {
				_, _ = fmt.Fprintf(w, "event: done\ndata: {\"state\":%q}\n\n", e.State)
				flusher.Flush()
				return
			}
		}
		_, _ = fmt.Fprint(w, ": keep-alive\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

// --- review items --------------------------------------------------------------------------

type reviewView struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	AccountID   *string         `json:"account_id"`
	AccountName string          `json:"account_name,omitempty"`
	ISIN        string          `json:"isin"`
	Symbol      string          `json:"symbol"`
	Message     string          `json:"message"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Status      string          `json:"status"`
	Resolution  json.RawMessage `json:"resolution,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	ResolvedAt  *time.Time      `json:"resolved_at"`
}

func (s *Server) listReviewItems(w http.ResponseWriter, r *http.Request) {
	var status *string
	if st := r.URL.Query().Get("status"); st != "" {
		if st != "open" && st != "resolved" && st != "dismissed" {
			writeProblem(w, r, validation(map[string]string{"status": "open, resolved or dismissed"}))
			return
		}
		status = &st
	}
	rows, err := s.q.ListReviewItems(r.Context(), store.ListReviewItemsParams{UserID: UserID(r.Context()), Status: status})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	out := make([]reviewView, 0, len(rows))
	for _, it := range rows {
		v := reviewView{ID: it.ID, Kind: it.Kind, AccountID: it.AccountID, ISIN: it.Isin, Symbol: it.Symbol, Message: it.Message, Status: it.Status, CreatedAt: it.CreatedAt, ResolvedAt: it.ResolvedAt}
		if it.AccountName != nil {
			v.AccountName = *it.AccountName
		}
		if len(it.Payload) > 0 {
			v.Payload = it.Payload
		}
		if len(it.Resolution) > 0 {
			v.Resolution = it.Resolution
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

// resolveReviewItem applies the user's answer and triggers a recompute. The body depends on
// the item kind:
//
//	MISSING_BUY_HISTORY: {"acquisition_date":"2019-03-01","cost_total":"12345.00"} or {"dismiss":true}
//	MISSING_FMV_2018:    {"fmv":"605.00"}
//	UNKNOWN_ISIN:        {"isin":"INE009A01021"}
//	TRANSFER_DETAILS:    {"acquisition_date":"2019-03-01","cost_total":"12345.00"}
//	others:              {"dismiss":true}
func (s *Server) resolveReviewItem(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dismiss         bool   `json:"dismiss"`
		AcquisitionDate string `json:"acquisition_date"`
		CostTotal       string `json:"cost_total"`
		Quantity        string `json:"quantity"`
		FMV             string `json:"fmv"`
		ISIN            string `json:"isin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	uid := UserID(r.Context())
	item, err := s.q.GetReviewItem(r.Context(), store.GetReviewItemParams{ID: chi.URLParam(r, "id"), UserID: uid})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	if item.Status != "open" {
		writeProblem(w, r, conflict("this item is already "+item.Status))
		return
	}
	var payload map[string]string
	_ = json.Unmarshal(item.Payload, &payload)
	resolution := map[string]any{}
	status := "resolved"

	err = store.WithTx(r.Context(), s.pool, func(tx pgx.Tx, q *store.Queries) error {
		switch {
		case body.Dismiss:
			status = "dismissed"
			resolution["dismissed"] = true
		case item.Kind == string(domain.ReviewMissingBuy):
			date, cost, qty, err := parseLotInput(body.AcquisitionDate, body.CostTotal, firstNonEmpty(body.Quantity, payload["quantity"]))
			if err != nil {
				return err
			}
			if item.AccountID == nil {
				return badRequest("item has no account")
			}
			lot, err := q.InsertOpeningLot(r.Context(), store.InsertOpeningLotParams{UserID: uid, AccountID: *item.AccountID, Isin: item.Isin, Symbol: item.Symbol,
				Quantity: qty, CostTotal: cost, AcquisitionDate: date.Time(), Note: "entered from review item " + item.ID})
			if err != nil {
				return err
			}
			resolution["opening_lot_id"] = lot.ID
			resolution["acquisition_date"], resolution["cost_total"], resolution["quantity"] = date.String(), cost.String(), qty.String()
		case item.Kind == string(domain.ReviewMissingFMV):
			fmv, err := decimal.NewFromString(strings.TrimSpace(body.FMV))
			if err != nil || !fmv.IsPositive() {
				return validation(map[string]string{"fmv": "a positive price is required"})
			}
			if err := q.UpsertInstrumentOverride(r.Context(), store.UpsertInstrumentOverrideParams{UserID: uid, Isin: item.Isin, Symbol: item.Symbol, Fmv20180131: decimal.NullDecimal{Decimal: fmv, Valid: true}}); err != nil {
				return err
			}
			resolution["fmv_2018_01_31"] = fmv.String()
		case item.Kind == string(domain.ReviewUnknownISIN):
			isin := strings.ToUpper(strings.TrimSpace(body.ISIN))
			if len(isin) != 12 {
				return validation(map[string]string{"isin": "an ISIN has 12 characters, e.g. INE009A01021"})
			}
			if err := q.UpsertInstrumentOverride(r.Context(), store.UpsertInstrumentOverrideParams{UserID: uid, Isin: isin, Symbol: item.Symbol}); err != nil {
				return err
			}
			resolution["isin"] = isin
			// Re-parse the upload that raised the item so the rows get their ISIN.
			if item.JobID != nil {
				if j, err := q.GetJobAny(r.Context(), *item.JobID); err == nil && j.UploadID != nil {
					nj, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, UploadID: j.UploadID, Kind: jobs.KindProcessUpload, State: jobs.StateUploaded, Payload: []byte(`{"reason":"isin resolved"}`)})
					if err != nil {
						return err
					}
					if _, err := s.jobs.River.InsertTx(r.Context(), tx, jobs.ProcessUploadArgs{UserID: uid, UploadID: *j.UploadID, JobID: nj.ID}, nil); err != nil {
						return err
					}
					r.Header.Set("X-Recompute-Job", nj.ID)
				}
			}
		case item.Kind == string(domain.ReviewTransferDetails):
			date, cost, _, err := parseLotInput(body.AcquisitionDate, body.CostTotal, payload["quantity"])
			if err != nil {
				return err
			}
			t := date.Time()
			n, err := q.SetTradeOriginalByKey(r.Context(), store.SetTradeOriginalByKeyParams{UserID: uid, SourceRowHash: item.TradeKey, OriginalAcquisitionDate: &t, OriginalCost: decimal.NullDecimal{Decimal: cost, Valid: true}})
			if err != nil {
				return err
			}
			if n == 0 {
				return notFound("transfer trade")
			}
			resolution["acquisition_date"], resolution["cost_total"] = date.String(), cost.String()
		default:
			return badRequest("this item can only be dismissed")
		}
		res, _ := json.Marshal(resolution)
		if err := q.ResolveReviewItem(r.Context(), store.ResolveReviewItemParams{ID: item.ID, UserID: uid, Status: status, Resolution: res}); err != nil {
			return err
		}
		if err := q.InsertAudit(r.Context(), store.InsertAuditParams{UserID: &uid, Entity: "review_item", EntityID: item.ID, Action: status, After: res}); err != nil {
			return err
		}
		if r.Header.Get("X-Recompute-Job") == "" {
			job, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, Kind: jobs.KindRecompute, State: jobs.StateNormalized, Payload: []byte(`{"reason":"review item resolved"}`)})
			if err != nil {
				return err
			}
			if _, err := q.InsertJobEvent(r.Context(), store.InsertJobEventParams{JobID: job.ID, UserID: uid, State: jobs.StateNormalized, Message: "Recomputing after your answer"}); err != nil {
				return err
			}
			r.Header.Set("X-Recompute-Job", job.ID)
			return jobs.EnqueueRecompute(r.Context(), s.jobs.River, tx, uid)
		}
		return nil
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": status, "resolution": resolution, "job_id": r.Header.Get("X-Recompute-Job")})
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func parseLotInput(dateS, costS, qtyS string) (domain.Date, decimal.Decimal, decimal.Decimal, error) {
	fields := map[string]string{}
	date, err := domain.ParseDate(dateS)
	if err != nil {
		fields["acquisition_date"] = "use YYYY-MM-DD"
	}
	cost, err := decimal.NewFromString(strings.TrimSpace(costS))
	if err != nil || cost.IsNegative() {
		fields["cost_total"] = "a non-negative amount is required"
	}
	qty, err := decimal.NewFromString(strings.TrimSpace(qtyS))
	if err != nil || !qty.IsPositive() {
		fields["quantity"] = "a positive quantity is required"
	}
	if len(fields) > 0 {
		return domain.Date{}, decimal.Zero, decimal.Zero, validation(fields)
	}
	return date, cost, qty, nil
}

// addManualTrade records a buyback tender or an off-market transfer that tradebooks omit.
func (s *Server) addManualTrade(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID               string `json:"account_id"`
		ISIN                    string `json:"isin"`
		Symbol                  string `json:"symbol"`
		Kind                    string `json:"kind"` // BUYBACK | TRANSFER_IN | TRANSFER_OUT | REGULAR
		Side                    string `json:"side"`
		Quantity                string `json:"quantity"`
		Price                   string `json:"price"`
		TradeDate               string `json:"trade_date"`
		OriginalAcquisitionDate string `json:"original_acquisition_date"`
		OriginalCost            string `json:"original_cost"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	uid := UserID(r.Context())
	fields := map[string]string{}
	if _, err := s.q.GetAccount(r.Context(), store.GetAccountParams{ID: body.AccountID, UserID: uid}); err != nil {
		fields["account_id"] = "no such account"
	}
	body.ISIN = strings.ToUpper(strings.TrimSpace(body.ISIN))
	if len(body.ISIN) != 12 {
		fields["isin"] = "12-character ISIN required"
	}
	kind := domain.TradeKind(strings.ToUpper(body.Kind))
	switch kind {
	case domain.KindBuyback, domain.KindTransferIn, domain.KindTransferOut, domain.KindRegular:
	default:
		fields["kind"] = "BUYBACK, TRANSFER_IN, TRANSFER_OUT or REGULAR"
	}
	side := domain.Side(strings.ToUpper(body.Side))
	if kind == domain.KindBuyback || kind == domain.KindTransferOut {
		side = domain.Sell
	} else if kind == domain.KindTransferIn {
		side = domain.Buy
	} else if side != domain.Buy && side != domain.Sell {
		fields["side"] = "BUY or SELL"
	}
	qty, err := decimal.NewFromString(strings.TrimSpace(body.Quantity))
	if err != nil || !qty.IsPositive() {
		fields["quantity"] = "positive quantity required"
	}
	price, err := decimal.NewFromString(strings.TrimSpace(firstNonEmpty(body.Price, "0")))
	if err != nil || price.IsNegative() {
		fields["price"] = "non-negative price required"
	}
	date, err := domain.ParseDate(body.TradeDate)
	if err != nil {
		fields["trade_date"] = "use YYYY-MM-DD"
	}
	t := domain.Trade{AccountID: body.AccountID, ISIN: body.ISIN, Symbol: strings.ToUpper(strings.TrimSpace(body.Symbol)), Segment: domain.SegEquity, Kind: kind, Side: side, Quantity: qty, Price: price, TradeDate: date}
	if t.Symbol == "" {
		t.Symbol = t.ISIN
	}
	if kind == domain.KindTransferIn && body.OriginalAcquisitionDate != "" {
		od, err := domain.ParseDate(body.OriginalAcquisitionDate)
		if err != nil {
			fields["original_acquisition_date"] = "use YYYY-MM-DD"
		}
		oc, err := decimal.NewFromString(strings.TrimSpace(body.OriginalCost))
		if err != nil || oc.IsNegative() {
			fields["original_cost"] = "non-negative amount required"
		}
		t.OriginalAcquisitionDate, t.OriginalCost = od, oc
	}
	if len(fields) > 0 {
		writeProblem(w, r, validation(fields))
		return
	}
	t.ID = domain.SourceRowHash("manual", body.AccountID, string(kind), string(side), t.ISIN, qty.String(), price.String(), date.String())
	err = store.WithTx(r.Context(), s.pool, func(tx pgx.Tx, q *store.Queries) error {
		raw, _ := json.Marshal(map[string]string{"source": "manual"})
		p := store.InsertManualTradeParams{UserID: uid, AccountID: body.AccountID, SourceRowHash: t.ID, Isin: t.ISIN, Symbol: t.Symbol, Segment: string(t.Segment), Kind: string(kind), Side: string(side),
			Quantity: qty, Price: price, TradeDate: date.Time(), Raw: raw}
		if !t.OriginalAcquisitionDate.IsZero() {
			od := t.OriginalAcquisitionDate.Time()
			p.OriginalAcquisitionDate = &od
			p.OriginalCost = decimal.NullDecimal{Decimal: t.OriginalCost, Valid: true}
		}
		if _, err := q.InsertManualTrade(r.Context(), p); err != nil {
			return err
		}
		if err := q.InsertAudit(r.Context(), store.InsertAuditParams{UserID: &uid, Entity: "trade", EntityID: t.ID, Action: "manual_add", After: raw}); err != nil {
			return err
		}
		job, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, Kind: jobs.KindRecompute, State: jobs.StateNormalized, Payload: []byte(`{"reason":"manual trade"}`)})
		if err != nil {
			return err
		}
		r.Header.Set("X-Recompute-Job", job.ID)
		return jobs.EnqueueRecompute(r.Context(), s.jobs.River, tx, uid)
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"trade_id": t.ID, "job_id": r.Header.Get("X-Recompute-Job")})
}
