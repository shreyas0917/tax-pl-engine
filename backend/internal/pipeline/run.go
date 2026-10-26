// Package pipeline orchestrates the pure stages: parse -> normalize -> match -> classify ->
// summarise. Run itself is pure (bytes in, report out) so the CLI, the golden tests and the
// background worker all execute exactly the same code path. Deterministic replay follows.
package pipeline

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/charges"
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
	// EstimateCharges fills in charges from the dated schedule for trades that carry none.
	// Estimated values are labelled "estimated" and shown as such.
	EstimateCharges bool
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
	if req.EstimateCharges {
		if err := estimateCharges(trades, req, instruments, cfg); err != nil {
			return nil, err
		}
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

// estimateCharges runs a first matching pass to learn which quantities were squared off
// intraday (different STT), then attaches estimated charges to every regular trade that has
// none. Brokerage is charged once per trade; the per-value levies are split by quantity.
func estimateCharges(trades []domain.Trade, req Request, instruments map[string]domain.Instrument, cfg matcher.Config) error {
	sched, err := charges.Load()
	if err != nil {
		return err
	}
	first, err := matcher.Match(matcher.Input{Trades: trades, CorporateActions: req.CorporateActions, Instruments: instruments, OpeningLots: req.OpeningLots, Config: cfg})
	if err != nil {
		return err
	}
	intradayQty := map[string]decimal.Decimal{}
	for _, m := range first.Intraday {
		intradayQty[m.BuyTradeID] = intradayQty[m.BuyTradeID].Add(m.Quantity)
		intradayQty[m.SellTradeID] = intradayQty[m.SellTradeID].Add(m.Quantity)
	}
	for i := range trades {
		t := &trades[i]
		inScope := t.Segment.IsEquityLike() || t.Segment.IsFNO()
		if t.Charges != nil || (t.Kind != "" && t.Kind != domain.KindRegular) || !inScope {
			continue
		}
		iq := intradayQty[t.ID]
		if t.Segment.IsFNO() || iq.IsZero() || iq.Equal(t.Quantity) {
			c, err := sched.Estimate(*t, charges.ClassOf(*t, iq.IsPositive()))
			if err != nil {
				return err
			}
			t.Charges = &c
			continue
		}
		// Mixed: brokerage on the whole order (class of the larger part), levies by part.
		dq := t.Quantity.Sub(iq)
		major := charges.ClassDelivery
		if iq.GreaterThan(dq) {
			major = charges.ClassIntraday
		}
		whole, err := sched.Estimate(*t, major)
		if err != nil {
			return err
		}
		del, intra := *t, *t
		del.Quantity, intra.Quantity = dq, iq
		cd, err := sched.Estimate(del, charges.ClassDelivery)
		if err != nil {
			return err
		}
		ci, err := sched.Estimate(intra, charges.ClassIntraday)
		if err != nil {
			return err
		}
		c := domain.Charges{Source: "estimated", Brokerage: whole.Brokerage, STT: cd.STT.Add(ci.STT), ExchangeFee: cd.ExchangeFee.Add(ci.ExchangeFee),
			SEBIFee: cd.SEBIFee.Add(ci.SEBIFee), StampDuty: cd.StampDuty.Add(ci.StampDuty)}
		c.GST = domain.RoundMoney(c.Brokerage.Add(c.ExchangeFee).Add(c.SEBIFee).Mul(sched.GSTPct))
		t.Charges = &c
	}
	return nil
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
