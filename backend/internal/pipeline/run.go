// Package pipeline orchestrates the pure stages: parse -> normalize -> match -> classify ->
// summarise. Run itself is pure (bytes in, report out) so the CLI, the golden tests and the
// background worker all execute exactly the same code path. Deterministic replay follows.
package pipeline

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/matcher"
	"github.com/shreyas0917/taxpl/internal/normalize"
	"github.com/shreyas0917/taxpl/internal/parser"
	"github.com/shreyas0917/taxpl/internal/tax"
)

// File is one uploaded tradebook bound to an account.
type File struct {
	Name      string
	AccountID string
	Broker    string // "" = auto-detect
	Data      []byte
}

// Request is everything Run needs.
type Request struct {
	Files            []File
	FY               string // "" = every year present in the data
	CorporateActions []domain.CorporateAction
	Instruments      map[string]domain.Instrument
	SymbolToISIN     map[string]string
	OpeningLots      []domain.Lot
	// ExtraTrades lets callers inject non-file trades (transfers, opening lots as trades, buybacks).
	ExtraTrades []domain.Trade
	AsOf        domain.Date
}

// FileResult reports what happened to one file.
type FileResult struct {
	Name       string              `json:"name"`
	AccountID  string              `json:"account_id"`
	Broker     string              `json:"broker"`
	Confidence float64             `json:"confidence"`
	Rows       int                 `json:"rows"`
	Trades     int                 `json:"trades"`
	Duplicates int                 `json:"duplicates"`
	Issues     []parser.ParseIssue `json:"issues"`
}

// Report is the complete output.
type Report struct {
	GeneratedFor  string                 `json:"generated_for"` // FY filter or "all"
	RulesVersions map[string]string      `json:"rules_versions"`
	Files         []FileResult           `json:"files"`
	Summaries     []tax.Summary          `json:"summaries"`
	Gains         []domain.Gain          `json:"gains"`
	Trades        []domain.Trade         `json:"trades"`
	Lots          []domain.Lot           `json:"lots"`
	Matches       []domain.LotMatch      `json:"matches"`
	Intraday      []domain.IntradayMatch `json:"intraday"`
	FNO           []domain.FNOMatch      `json:"fno"`
	OpenPositions []domain.OpenPosition  `json:"open_positions"`
	Review        []domain.ReviewItem    `json:"review"`
	AuditNotes    []string               `json:"audit_notes"`
	Disclaimer    string                 `json:"disclaimer"`
}

// Disclaimer shown on every report.
const Disclaimer = "This report organises your trade data; it is not tax advice. Verify every figure with a qualified Chartered Accountant before filing."

// Run executes the whole pure pipeline.
func Run(req Request) (*Report, error) {
	rep := &Report{GeneratedFor: req.FY, RulesVersions: map[string]string{}, Disclaimer: Disclaimer}
	if rep.GeneratedFor == "" {
		rep.GeneratedFor = "all"
	}
	instruments := req.Instruments
	if instruments == nil {
		instruments = map[string]domain.Instrument{}
	}

	var trades []domain.Trade
	var review []domain.ReviewItem
	for _, f := range req.Files {
		fr := FileResult{Name: f.Name, AccountID: f.AccountID, Broker: f.Broker}
		var p parser.Parser
		if f.Broker != "" {
			pp, ok := parser.Get(f.Broker)
			if !ok {
				return nil, fmt.Errorf("file %s: unknown broker %q (known: %v)", f.Name, f.Broker, parser.Brokers())
			}
			p = pp
			fr.Confidence = 1
		} else {
			pp, det, ok := parser.Best(f.Data)
			if !ok {
				return nil, fmt.Errorf("file %s: could not detect the broker format; pass the broker explicitly", f.Name)
			}
			p, fr.Broker, fr.Confidence = pp, det.Broker, det.Confidence
		}
		rows, issues, err := p.Parse(bytes.NewReader(f.Data))
		if err != nil {
			return nil, fmt.Errorf("file %s: %w", f.Name, err)
		}
		fr.Rows = len(rows) + len(issues)
		fr.Issues = issues
		for _, is := range issues {
			review = append(review, domain.ReviewItem{ID: domain.SourceRowHash("issue", f.Name, fmt.Sprint(is.Line)), Kind: domain.ReviewParseIssue, AccountID: f.AccountID,
				Message: fmt.Sprintf("%s line %d could not be read: %s", f.Name, is.Line, is.Reason), Payload: map[string]string{"file": f.Name, "line": fmt.Sprint(is.Line)}})
		}
		n := normalize.Normalize(normalize.Input{AccountID: f.AccountID, Broker: fr.Broker, SourceFile: f.Name, Rows: rows, Instruments: instruments, SymbolToISIN: req.SymbolToISIN})
		fr.Trades = len(n.Trades)
		fr.Duplicates = n.Duplicates
		trades = append(trades, n.Trades...)
		review = append(review, n.Review...)
		rep.Files = append(rep.Files, fr)
	}
	trades = append(trades, req.ExtraTrades...)
	trades = dedupe(trades)

	cfg := matcher.Config{AsOf: req.AsOf}
	// Matching behaviour (netting, grandfathering cutoff) is the same across the years we
	// support; take it from the latest rules file so a future change is data, not code.
	years := tax.Years()
	if len(years) > 0 {
		latest, err := tax.Load(years[len(years)-1])
		if err != nil {
			return nil, err
		}
		c := tax.ConfigFor(latest)
		cfg.IntradayNetting, cfg.GrandfatheringCutoff = c.IntradayNetting, c.GrandfatheringCutoff
	}
	mres, err := matcher.Match(matcher.Input{Trades: trades, CorporateActions: req.CorporateActions, Instruments: instruments, OpeningLots: req.OpeningLots, Config: cfg})
	if err != nil {
		return nil, err
	}
	cls := tax.Classify(mres)
	review = append(review, mres.Review...)
	review = append(review, cls.Review...)

	for _, g := range cls.Gains {
		if req.FY == "" || g.FY == req.FY {
			rep.Gains = append(rep.Gains, g)
		}
	}
	for fy, v := range cls.YearsUsed {
		if req.FY == "" || fy == req.FY {
			rep.RulesVersions[fy] = v
		}
	}
	if req.FY != "" {
		if _, ok := rep.RulesVersions[req.FY]; !ok {
			r, err := tax.Load(req.FY)
			if err != nil {
				return nil, err
			}
			rep.RulesVersions[req.FY] = r.VersionString()
		}
	}
	fys := make([]string, 0, len(rep.RulesVersions))
	for fy := range rep.RulesVersions {
		fys = append(fys, fy)
	}
	sort.Strings(fys)
	for _, fy := range fys {
		s, err := tax.Summarise(fy, rep.Gains)
		if err != nil {
			return nil, err
		}
		rep.Summaries = append(rep.Summaries, s)
	}

	rep.Trades = mres.Trades
	rep.Lots = mres.Lots
	rep.Matches = mres.Matches
	rep.Intraday = mres.Intraday
	rep.FNO = mres.FNO
	rep.OpenPositions = mres.OpenPositions
	rep.AuditNotes = mres.AuditNotes
	if len(mres.OpenPositions) > 0 {
		for _, op := range mres.OpenPositions {
			review = append(review, domain.ReviewItem{ID: domain.SourceRowHash("open", op.AccountID, op.TradeID), Kind: domain.ReviewOpenPosition, AccountID: op.AccountID, Symbol: op.Symbol, TradeID: op.TradeID,
				Message: fmt.Sprintf("%s units of %s (%s) opened on %s have no closing trade. If the contract expired or was exercised, add the closing row from your broker's P&L.", op.Quantity, op.ContractKey, op.Side, op.OpenDate)})
		}
	}
	sort.SliceStable(review, func(i, j int) bool { return review[i].ID < review[j].ID })
	rep.Review = review
	return rep, nil
}

func dedupe(ts []domain.Trade) []domain.Trade {
	seen := map[string]bool{}
	out := ts[:0]
	for _, t := range ts {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out
}
