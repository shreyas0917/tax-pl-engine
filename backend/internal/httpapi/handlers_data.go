package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/jobs"
	"github.com/shreyas0917/taxpl/internal/parser"
	"github.com/shreyas0917/taxpl/internal/store"
	"github.com/shreyas0917/taxpl/internal/tax"
)

// --- rules / brokers ----------------------------------------------------------------------

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	type ruleView struct {
		Year         string            `json:"year"`
		Label        string            `json:"label"`
		Law          string            `json:"law"`
		Version      string            `json:"rules_version"`
		VerifiedBy   string            `json:"verified_by"`
		References   map[string]string `json:"references"`
		Periods      []tax.RatePeriod  `json:"periods"`
		LTCGExempt   decimal.Decimal   `json:"ltcg_exemption"`
		AuditLimit   decimal.Decimal   `json:"audit_threshold"`
		AuditLowCash decimal.Decimal   `json:"audit_threshold_low_cash"`
	}
	var out []ruleView
	for _, y := range tax.Years() {
		rl, err := tax.Load(y)
		if err != nil {
			writeProblem(w, r, err)
			return
		}
		out = append(out, ruleView{Year: rl.Year, Label: rl.YearLabel, Law: rl.Law, Version: rl.VersionString(), VerifiedBy: rl.VerifiedBy, References: rl.References,
			Periods: rl.Equity.Periods, LTCGExempt: rl.Equity.LTCGExemption, AuditLimit: rl.FNO.AuditThreshold, AuditLowCash: rl.FNO.AuditThresholdLowCash})
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) listBrokers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"items": parser.Brokers()})
}

// --- summary ------------------------------------------------------------------------------

func fyParam(r *http.Request) (string, error) {
	fy := strings.TrimSpace(r.URL.Query().Get("fy"))
	if fy == "" {
		return "", validation(map[string]string{"fy": "required, e.g. 2025-26"})
	}
	if _, err := tax.Load(fy); err != nil {
		return "", validation(map[string]string{"fy": "no rules for this year; available: " + strings.Join(tax.Years(), ", ")})
	}
	return fy, nil
}

func (s *Server) loadGains(r *http.Request, fy string) ([]domain.Gain, map[string]string, error) {
	rows, err := s.q.ListGainsForFY(r.Context(), store.ListGainsForFYParams{UserID: UserID(r.Context()), Fy: fy})
	if err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	out := make([]domain.Gain, 0, len(rows))
	for _, row := range rows {
		g, err := jobs.GainFromRow(row.Explain)
		if err != nil {
			return nil, nil, err
		}
		names[row.AccountID] = row.AccountName
		out = append(out, g)
	}
	return out, names, nil
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	fy, err := fyParam(r)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	gains, names, err := s.loadGains(r, fy)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	sum, err := tax.Summarise(fy, gains)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	for i := range sum.Accounts {
		if n, ok := names[sum.Accounts[i].AccountID]; ok {
			sum.Accounts[i].AccountID = n
		}
	}
	uid := UserID(r.Context())
	open, _ := s.q.CountOpenReviewItems(r.Context(), uid)
	fys, _ := s.q.ListGainFYs(r.Context(), uid)
	var comp any
	if c, err := s.q.LatestComputation(r.Context(), uid); err == nil {
		comp = map[string]any{"id": c.ID, "checksum": c.Checksum, "rules_versions": json.RawMessage(c.RulesVersions), "trade_count": c.TradeCount, "created_at": c.CreatedAt}
	}
	writeJSON(w, 200, map[string]any{"summary": sum, "open_review_items": open, "years_with_data": fys, "computation": comp, "disclaimer": "This is not tax advice. Verify with a qualified CA."})
}

// --- gains --------------------------------------------------------------------------------

type cursor struct {
	Date string `json:"d"`
	Key  string `json:"k"`
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	if s == "" {
		return cursor{Date: "0001-01-01"}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, badRequest("invalid cursor")
	}
	var c cursor
	if json.Unmarshal(b, &c) != nil || c.Date == "" {
		return cursor{}, badRequest("invalid cursor")
	}
	return c, nil
}

func pageSize(r *http.Request) int32 {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		n = 100
	}
	if n > 1000 {
		n = 1000
	}
	return int32(n)
}

func optStr(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

type gainView struct {
	ID                string          `json:"id"`
	FY                string          `json:"fy"`
	Category          string          `json:"category"`
	AccountID         string          `json:"account_id"`
	AccountName       string          `json:"account_name"`
	ISIN              string          `json:"isin"`
	Symbol            string          `json:"symbol"`
	ContractKey       string          `json:"contract_key,omitempty"`
	SaleDate          string          `json:"sale_date"`
	AcquisitionDate   string          `json:"acquisition_date,omitempty"`
	Quantity          decimal.Decimal `json:"quantity"`
	SaleValue         decimal.Decimal `json:"sale_value"`
	CostOfAcquisition decimal.Decimal `json:"cost_of_acquisition"`
	ChargesDeductible decimal.Decimal `json:"charges_deductible"`
	Gain              decimal.Decimal `json:"gain"`
	HoldingDays       int32           `json:"holding_days"`
	RatePeriod        string          `json:"rate_period,omitempty"`
	BuybackRegime     string          `json:"buyback_regime,omitempty"`
	Grandfathered     *bool           `json:"grandfathered,omitempty"`
	RulesVersion      string          `json:"rules_version"`
}

func (s *Server) listGains(w http.ResponseWriter, r *http.Request) {
	fy, err := fyParam(r)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	cur, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	curDate, err := domain.ParseDate(cur.Date)
	if err != nil {
		writeProblem(w, r, badRequest("invalid cursor"))
		return
	}
	size := pageSize(r)
	rows, err := s.q.ListGainsPage(r.Context(), store.ListGainsPageParams{UserID: UserID(r.Context()), Fy: fy, Category: optStr(r.URL.Query().Get("category")),
		AccountID: optStr(r.URL.Query().Get("account")), Isin: optStr(strings.ToUpper(r.URL.Query().Get("isin"))), CursorDate: curDate.Time(), CursorKey: cur.Key, PageSize: size + 1})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	next := ""
	if len(rows) > int(size) {
		rows = rows[:size]
		last := rows[len(rows)-1]
		next = encodeCursor(cursor{Date: domain.DateOf(last.SaleDate).String(), Key: last.GainKey})
	}
	out := make([]gainView, 0, len(rows))
	for _, row := range rows {
		v := gainView{ID: row.ID, FY: row.Fy, Category: row.Category, AccountID: row.AccountID, AccountName: row.AccountName, ISIN: row.Isin, Symbol: row.Symbol, ContractKey: row.ContractKey,
			SaleDate: domain.DateOf(row.SaleDate).String(), Quantity: row.Quantity, SaleValue: row.SaleValue, CostOfAcquisition: row.CostOfAcquisition, ChargesDeductible: row.ChargesDeductible,
			Gain: row.Gain, HoldingDays: row.HoldingDays, RatePeriod: row.RatePeriod, BuybackRegime: row.BuybackRegime, RulesVersion: row.RulesVersion}
		if row.AcquisitionDate != nil {
			v.AcquisitionDate = domain.DateOf(*row.AcquisitionDate).String()
		}
		if g, err := jobs.GainFromRow(row.Explain); err == nil && g.Grandfathering != nil {
			applied := g.Grandfathering.Applied
			v.Grandfathered = &applied
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"items": out, "next_cursor": next})
}

// explainGain returns the "why this number" tree: the gain's derivation plus the matched lot(s).
func (s *Server) explainGain(w http.ResponseWriter, r *http.Request) {
	row, err := s.q.GetGain(r.Context(), store.GetGainParams{ID: chi.URLParam(r, "id"), UserID: UserID(r.Context())})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	g, err := jobs.GainFromRow(row.Explain)
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	req, err := jobs.BuildRequest(r.Context(), s.q, UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	tree, err := jobs.Explain(req, g, row.AccountName)
	if err != nil {
		writeProblem(w, r, &APIError{Status: 404, Title: "Not found", Detail: "this gain is no longer part of the current computation; refresh and try again"})
		return
	}
	writeJSON(w, 200, tree)
}

// --- trades / holdings ----------------------------------------------------------------------

func (s *Server) listTrades(w http.ResponseWriter, r *http.Request) {
	cur, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	curDate, err := domain.ParseDate(cur.Date)
	if err != nil {
		writeProblem(w, r, badRequest("invalid cursor"))
		return
	}
	var start, end *time.Time
	if fy := r.URL.Query().Get("fy"); fy != "" {
		a, b, err := domain.FinancialYearBounds(fy)
		if err != nil {
			writeProblem(w, r, validation(map[string]string{"fy": err.Error()}))
			return
		}
		at, bt := a.Time(), b.Time()
		start, end = &at, &bt
	}
	size := pageSize(r)
	rows, err := s.q.ListTradesPage(r.Context(), store.ListTradesPageParams{UserID: UserID(r.Context()), FyStart: start, FyEnd: end, AccountID: optStr(r.URL.Query().Get("account")),
		Isin: optStr(strings.ToUpper(r.URL.Query().Get("isin"))), CursorDate: curDate.Time(), CursorID: cur.Key, PageSize: size + 1})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	next := ""
	if len(rows) > int(size) {
		rows = rows[:size]
		last := rows[len(rows)-1]
		next = encodeCursor(cursor{Date: domain.DateOf(last.TradeDate).String(), Key: last.ID})
	}
	type tradeView struct {
		ID          string          `json:"id"`
		AccountID   string          `json:"account_id"`
		AccountName string          `json:"account_name"`
		Broker      string          `json:"broker"`
		ISIN        string          `json:"isin"`
		Symbol      string          `json:"symbol"`
		Segment     string          `json:"segment"`
		Kind        string          `json:"kind"`
		Side        string          `json:"side"`
		Quantity    decimal.Decimal `json:"quantity"`
		Price       decimal.Decimal `json:"price"`
		TradeDate   string          `json:"trade_date"`
		TradeTime   string          `json:"trade_time"`
		Exchange    string          `json:"exchange"`
		Expiry      string          `json:"expiry,omitempty"`
		Strike      string          `json:"strike,omitempty"`
		OptionType  string          `json:"option_type,omitempty"`
	}
	out := make([]tradeView, 0, len(rows))
	for _, t := range rows {
		v := tradeView{ID: t.ID, AccountID: t.AccountID, AccountName: t.AccountName, Broker: t.AccountBroker, ISIN: t.Isin, Symbol: t.Symbol, Segment: t.Segment, Kind: t.Kind, Side: t.Side,
			Quantity: t.Quantity, Price: t.Price, TradeDate: domain.DateOf(t.TradeDate).String(), TradeTime: t.TradeTime, Exchange: t.Exchange, OptionType: t.OptionType}
		if t.Expiry != nil {
			v.Expiry = domain.DateOf(*t.Expiry).String()
		}
		if t.Strike.Valid {
			v.Strike = t.Strike.Decimal.String()
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"items": out, "next_cursor": next})
}

func (s *Server) listHoldings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListOpenLots(r.Context(), UserID(r.Context()))
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	type lotView struct {
		ID               string          `json:"id"`
		AccountID        string          `json:"account_id"`
		AccountName      string          `json:"account_name"`
		ISIN             string          `json:"isin"`
		Symbol           string          `json:"symbol"`
		Origin           string          `json:"origin"`
		QuantityOpen     decimal.Decimal `json:"quantity_open"`
		CostOpen         decimal.Decimal `json:"cost_open"`
		AcquisitionDate  string          `json:"acquisition_date"`
		GrandfatheredFMV *string         `json:"grandfathered_fmv,omitempty"`
	}
	out := make([]lotView, 0, len(rows))
	for _, l := range rows {
		v := lotView{ID: l.ID, AccountID: l.AccountID, AccountName: l.AccountName, ISIN: l.Isin, Symbol: l.Symbol, Origin: l.Origin, QuantityOpen: l.QuantityOpen, CostOpen: l.CostOpen, AcquisitionDate: domain.DateOf(l.AcquisitionDate).String()}
		if l.GrandfatheredFmv.Valid {
			f := l.GrandfatheredFmv.Decimal.String()
			v.GrandfatheredFMV = &f
		}
		out = append(out, v)
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

// --- reports ------------------------------------------------------------------------------

type reportView struct {
	ID           string    `json:"id"`
	FY           string    `json:"fy"`
	Format       string    `json:"format"`
	Checksum     string    `json:"checksum"`
	RulesVersion string    `json:"rules_version"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
	JobID        *string   `json:"job_id"`
}

func reportViewOf(rp store.Report) reportView {
	return reportView{ID: rp.ID, FY: rp.Fy, Format: rp.Format, Checksum: rp.Checksum, RulesVersion: rp.RulesVersion, SizeBytes: rp.SizeBytes, CreatedAt: rp.CreatedAt, JobID: rp.JobID}
}

func (s *Server) createReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FY     string `json:"fy"`
		Format string `json:"format"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeProblem(w, r, err)
		return
	}
	if _, err := tax.Load(body.FY); err != nil {
		writeProblem(w, r, validation(map[string]string{"fy": "unknown year; available: " + strings.Join(tax.Years(), ", ")}))
		return
	}
	body.Format = strings.ToLower(body.Format)
	if _, ok := s.jobs.ReportWriters[body.Format]; !ok {
		writeProblem(w, r, validation(map[string]string{"format": "xlsx, pdf or json"}))
		return
	}
	uid := UserID(r.Context())
	var jobID string
	reportID := newID()
	err := store.WithTx(r.Context(), s.pool, func(tx pgx.Tx, q *store.Queries) error {
		payload, _ := json.Marshal(map[string]string{"fy": body.FY, "format": body.Format, "report_id": reportID})
		job, err := q.CreateJob(r.Context(), store.CreateJobParams{UserID: uid, Kind: jobs.KindReport, State: jobs.StateUploaded, Payload: payload})
		if err != nil {
			return err
		}
		jobID = job.ID
		if _, err := q.InsertJobEvent(r.Context(), store.InsertJobEventParams{JobID: job.ID, UserID: uid, State: jobs.StateUploaded, Message: "Report requested"}); err != nil {
			return err
		}
		_, err = s.jobs.River.InsertTx(r.Context(), tx, jobs.ReportArgs{UserID: uid, JobID: job.ID, ReportID: reportID, FY: body.FY, Format: body.Format}, &river.InsertOpts{MaxAttempts: jobs.MaxAttempts})
		return err
	})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	writeJSON(w, 202, map[string]any{"job_id": jobID, "fy": body.FY, "format": body.Format})
}

func (s *Server) listReports(w http.ResponseWriter, r *http.Request) {
	rows, err := s.q.ListReports(r.Context(), store.ListReportsParams{UserID: UserID(r.Context()), Limit: 100})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	out := make([]reportView, 0, len(rows))
	for _, rp := range rows {
		out = append(out, reportViewOf(rp))
	}
	writeJSON(w, 200, map[string]any{"items": out})
}

func (s *Server) downloadReport(w http.ResponseWriter, r *http.Request) {
	rp, err := s.q.GetReport(r.Context(), store.GetReportParams{ID: chi.URLParam(r, "id"), UserID: UserID(r.Context())})
	if err != nil {
		writeProblem(w, r, err)
		return
	}
	rc, err := s.blob.Get(r.Context(), rp.BlobKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProblem(w, r, notFound("report file"))
			return
		}
		writeProblem(w, r, err)
		return
	}
	defer func() { _ = rc.Close() }()
	types := map[string]string{"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "pdf": "application/pdf", "json": "application/json"}
	w.Header().Set("Content-Type", types[rp.Format])
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="taxpl-%s.%s"`, rp.Fy, rp.Format))
	w.Header().Set("X-Checksum-SHA256", rp.Checksum)
	w.Header().Set("X-Rules-Version", rp.RulesVersion)
	_, _ = io.Copy(w, rc)
}
