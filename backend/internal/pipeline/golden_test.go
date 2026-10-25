package pipeline_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/domain"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
)

// Golden cases live in backend/testdata/golden/<case>/ with:
//
//	case.json      - which files, which accounts, which FY, reference data
//	input/*.csv    - synthetic tradebooks
//	expected.json  - HAND-COMPUTED answer (never generated from the engine)
//
// Run with -dump to write actual.json next to expected.json for inspection.
var dump = flag.Bool("dump", false, "write actual.json for each golden case")

type caseFile struct {
	FY    string `json:"fy"`
	Files []struct {
		Name    string `json:"name"`
		Account string `json:"account"`
		Broker  string `json:"broker"`
	} `json:"files"`
	Instruments      []domain.Instrument      `json:"instruments"`
	SymbolToISIN     map[string]string        `json:"symbol_to_isin"`
	CorporateActions []domain.CorporateAction `json:"corporate_actions"`
	OpeningLots      []domain.Lot             `json:"opening_lots"`
	ExtraTrades      []domain.Trade           `json:"extra_trades"`
	AsOf             domain.Date              `json:"as_of"`
	Note             string                   `json:"note"`
}

// Expected is the hand-verified shape. Decimals are compared numerically.
type Expected struct {
	Summaries map[string]ExpectedSummary `json:"summaries"`
	Gains     []ExpectedGain             `json:"gains"`
	Review    []string                   `json:"review"`
	OpenLots  []ExpectedLot              `json:"open_lots"`
}

type ExpectedSummary struct {
	STCG            string `json:"stcg"`
	LTCG            string `json:"ltcg"`
	Intraday        string `json:"intraday"`
	FNO             string `json:"fno"`
	FNOTurnover     string `json:"fno_turnover"`
	BuybackDividend string `json:"buyback_dividend"`
	BuybackExempt   string `json:"buyback_exempt"`
	EstimatedTax    string `json:"estimated_tax"`
	LTCGPre         string `json:"ltcg_pre_split,omitempty"`  // FY 2024-25 only
	LTCGPost        string `json:"ltcg_post_split,omitempty"` // FY 2024-25 only
}

type ExpectedGain struct {
	FY              string `json:"fy"`
	Category        string `json:"category"`
	Account         string `json:"account"`
	Symbol          string `json:"symbol"`
	SaleDate        string `json:"sale_date"`
	AcquisitionDate string `json:"acquisition_date"`
	Quantity        string `json:"quantity"`
	SaleValue       string `json:"sale_value"`
	Cost            string `json:"cost"`
	Charges         string `json:"charges"`
	Gain            string `json:"gain"`
	Grandfathered   *bool  `json:"grandfathered,omitempty"`
}

type ExpectedLot struct {
	Account  string `json:"account"`
	Symbol   string `json:"symbol"`
	Quantity string `json:"quantity"`
	Cost     string `json:"cost"`
	Acquired string `json:"acquired"`
}

func TestGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "golden")
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		count++
		dir := filepath.Join(root, e.Name())
		t.Run(e.Name(), func(t *testing.T) { runGolden(t, dir) })
	}
	require.GreaterOrEqual(t, count, 8, "spec requires at least 8 golden cases")
}

func loadCase(t *testing.T, dir string) (pipeline.Request, caseFile) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "case.json"))
	require.NoError(t, err)
	var cf caseFile
	require.NoError(t, json.Unmarshal(b, &cf))
	req := pipeline.Request{FY: cf.FY, Instruments: map[string]domain.Instrument{}, SymbolToISIN: cf.SymbolToISIN,
		CorporateActions: cf.CorporateActions, OpeningLots: cf.OpeningLots, ExtraTrades: cf.ExtraTrades, AsOf: cf.AsOf}
	for _, inst := range cf.Instruments {
		req.Instruments[inst.ISIN] = inst
	}
	for _, f := range cf.Files {
		data, err := os.ReadFile(filepath.Join(dir, "input", f.Name))
		require.NoError(t, err)
		req.Files = append(req.Files, pipeline.File{Name: f.Name, AccountID: f.Account, Broker: f.Broker, Data: data})
	}
	return req, cf
}

func runGolden(t *testing.T, dir string) {
	req, _ := loadCase(t, dir)
	rep, err := pipeline.Run(req)
	require.NoError(t, err)
	actual := project(rep)
	if *dump {
		b, _ := json.MarshalIndent(actual, "", "  ")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "actual.json"), b, 0o644))
	}
	eb, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	require.NoError(t, err, "every golden case needs a hand-written expected.json")
	var exp Expected
	require.NoError(t, json.Unmarshal(eb, &exp))
	compare(t, exp, actual)

	// Determinism: running again yields a byte-identical report.
	rep2, err := pipeline.Run(req)
	require.NoError(t, err)
	b1, _ := jsonreport.Marshal(rep)
	b2, _ := jsonreport.Marshal(rep2)
	require.Equal(t, jsonreport.Checksum(b1), jsonreport.Checksum(b2), "replay must be byte-identical")
}

func project(rep *pipeline.Report) Expected {
	out := Expected{Summaries: map[string]ExpectedSummary{}}
	for _, s := range rep.Summaries {
		es := ExpectedSummary{STCG: fx(s.STCG), LTCG: fx(s.LTCG), Intraday: fx(s.Intraday), FNO: fx(s.FNO), FNOTurnover: fx(s.FNOTurnover),
			BuybackDividend: fx(s.BuybackDividend), BuybackExempt: fx(s.BuybackExempt), EstimatedTax: fx(s.EstimatedTax)}
		if len(s.Periods) == 2 {
			es.LTCGPre, es.LTCGPost = fx(s.Periods[0].LTCG), fx(s.Periods[1].LTCG)
		}
		out.Summaries[s.FY] = es
	}
	for _, g := range rep.Gains {
		eg := ExpectedGain{FY: g.FY, Category: string(g.Category), Account: g.AccountID, Symbol: g.Symbol, SaleDate: g.SaleDate.String(),
			AcquisitionDate: g.AcquisitionDate.String(), Quantity: fx(g.Quantity), SaleValue: fx(g.SaleValue), Cost: fx(g.CostOfAcquisition),
			Charges: fx(g.ChargesDeductible), Gain: fx(g.Gain)}
		if g.Grandfathering != nil {
			v := g.Grandfathering.Applied
			eg.Grandfathered = &v
		}
		out.Gains = append(out.Gains, eg)
	}
	for _, r := range rep.Review {
		out.Review = append(out.Review, string(r.Kind))
	}
	sort.Strings(out.Review)
	for _, l := range rep.Lots {
		if l.QuantityOpen.IsPositive() {
			out.OpenLots = append(out.OpenLots, ExpectedLot{Account: l.AccountID, Symbol: l.Symbol, Quantity: fx(l.QuantityOpen), Cost: fx(l.CostOpen), Acquired: l.AcquisitionDate.String()})
		}
	}
	return out
}

func fx(d decimal.Decimal) string { return d.StringFixed(4) }

func num(t *testing.T, s string) decimal.Decimal {
	t.Helper()
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	require.NoError(t, err, "bad decimal %q in expected.json", s)
	return d
}

func eqNum(t *testing.T, what, exp, act string) {
	t.Helper()
	if !num(t, exp).Equal(num(t, act)) {
		t.Errorf("%s: expected %s, got %s", what, exp, act)
	}
}

func compare(t *testing.T, exp, act Expected) {
	t.Helper()
	for fy, es := range exp.Summaries {
		as, ok := act.Summaries[fy]
		if !ok {
			t.Errorf("summary for %s missing (have %v)", fy, keys(act.Summaries))
			continue
		}
		eqNum(t, fy+" stcg", es.STCG, as.STCG)
		eqNum(t, fy+" ltcg", es.LTCG, as.LTCG)
		eqNum(t, fy+" intraday", es.Intraday, as.Intraday)
		eqNum(t, fy+" fno", es.FNO, as.FNO)
		eqNum(t, fy+" fno_turnover", es.FNOTurnover, as.FNOTurnover)
		eqNum(t, fy+" buyback_dividend", es.BuybackDividend, as.BuybackDividend)
		eqNum(t, fy+" buyback_exempt", es.BuybackExempt, as.BuybackExempt)
		eqNum(t, fy+" estimated_tax", es.EstimatedTax, as.EstimatedTax)
		if es.LTCGPre != "" || es.LTCGPost != "" {
			eqNum(t, fy+" ltcg_pre_split", es.LTCGPre, as.LTCGPre)
			eqNum(t, fy+" ltcg_post_split", es.LTCGPost, as.LTCGPost)
		}
	}
	for fy := range act.Summaries {
		if _, ok := exp.Summaries[fy]; !ok {
			t.Errorf("unexpected summary for %s", fy)
		}
	}
	if len(exp.Gains) != len(act.Gains) {
		t.Errorf("expected %d gain lines, got %d:\n%s", len(exp.Gains), len(act.Gains), describe(act.Gains))
	} else {
		for i := range exp.Gains {
			e, a := exp.Gains[i], act.Gains[i]
			pfx := fmt.Sprintf("gain[%d] %s %s", i, e.Symbol, e.SaleDate)
			if e.FY != a.FY || e.Category != a.Category || e.Account != a.Account || e.Symbol != a.Symbol || e.SaleDate != a.SaleDate || e.AcquisitionDate != a.AcquisitionDate {
				t.Errorf("%s: identity mismatch\n  expected %+v\n  actual   %+v", pfx, e, a)
				continue
			}
			eqNum(t, pfx+" quantity", e.Quantity, a.Quantity)
			eqNum(t, pfx+" sale_value", e.SaleValue, a.SaleValue)
			eqNum(t, pfx+" cost", e.Cost, a.Cost)
			eqNum(t, pfx+" charges", e.Charges, a.Charges)
			eqNum(t, pfx+" gain", e.Gain, a.Gain)
			if e.Grandfathered != nil {
				if a.Grandfathered == nil {
					t.Errorf("%s: expected grandfathering comparison, none made", pfx)
				} else if *e.Grandfathered != *a.Grandfathered {
					t.Errorf("%s: grandfathering applied expected %v, got %v", pfx, *e.Grandfathered, *a.Grandfathered)
				}
			}
		}
	}
	expReview := append([]string(nil), exp.Review...)
	sort.Strings(expReview)
	if strings.Join(expReview, ",") != strings.Join(act.Review, ",") {
		t.Errorf("review items: expected %v, got %v", expReview, act.Review)
	}
	if exp.OpenLots != nil {
		if len(exp.OpenLots) != len(act.OpenLots) {
			t.Errorf("open lots: expected %d, got %d: %+v", len(exp.OpenLots), len(act.OpenLots), act.OpenLots)
		} else {
			for i := range exp.OpenLots {
				e, a := exp.OpenLots[i], act.OpenLots[i]
				pfx := fmt.Sprintf("open_lot[%d] %s", i, e.Symbol)
				if e.Account != a.Account || e.Symbol != a.Symbol || e.Acquired != a.Acquired {
					t.Errorf("%s: identity mismatch expected %+v actual %+v", pfx, e, a)
					continue
				}
				eqNum(t, pfx+" quantity", e.Quantity, a.Quantity)
				eqNum(t, pfx+" cost", e.Cost, a.Cost)
			}
		}
	}
}

func describe(gs []ExpectedGain) string {
	var sb strings.Builder
	for _, g := range gs {
		fmt.Fprintf(&sb, "  %s %s %s %s qty %s sale %s cost %s gain %s\n", g.FY, g.Category, g.Account, g.Symbol, g.Quantity, g.SaleValue, g.Cost, g.Gain)
	}
	return sb.String()
}

func keys(m map[string]ExpectedSummary) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
