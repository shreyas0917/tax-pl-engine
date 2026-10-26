// Package reconcile compares a broker's current holdings statement with the engine's open
// lots for one account. Differences usually mean missing history (shares bought before the
// first uploaded tradebook, transfers, corporate actions not on file). Pure.
package reconcile

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Holding is one line of the broker's holdings statement.
type Holding struct {
	Line     int             `json:"line"`
	Symbol   string          `json:"symbol"`
	ISIN     string          `json:"isin"`
	Quantity decimal.Decimal `json:"quantity"`
	AvgPrice decimal.Decimal `json:"avg_price"` // zero when the statement has none
}

// ParseHoldings reads a loose CSV: it needs a quantity column and either an ISIN or a symbol
// column, and accepts the headings Zerodha, Groww, Upstox and Angel One use.
func ParseHoldings(r io.Reader) ([]Holding, []parser.ParseIssue, error) {
	cr := parser.NewCSVReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	cols := parser.NewColumns(header)
	qtyNames := []string{"quantity available", "quantity", "qty", "net qty", "net quantity", "holding quantity", "total quantity", "units"}
	isinNames := []string{"isin", "isin code"}
	symNames := []string{"symbol", "instrument", "scrip", "scrip name", "stock name", "stock", "name", "security"}
	priceNames := []string{"average price", "avg price", "avg. price", "average cost", "avg cost", "buy average", "average buy price", "nav"}
	hasQty, hasKey := false, false
	for _, n := range qtyNames {
		if cols.Has(n) {
			hasQty = true
		}
	}
	for _, n := range append(isinNames, symNames...) {
		if cols.Has(n) {
			hasKey = true
		}
	}
	if !hasQty || !hasKey {
		return nil, nil, fmt.Errorf("not a holdings file: need a quantity column and an ISIN or symbol column (got %s)", strings.Join(parser.NormaliseHeader(header), ", "))
	}
	var out []Holding
	var issues []parser.ParseIssue
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			continue
		}
		line, _ := cr.FieldPos(0)
		blank := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				blank = false
			}
		}
		if blank {
			continue
		}
		h := Holding{Line: line, Symbol: strings.ToUpper(cols.Get(rec, symNames...)), ISIN: strings.ToUpper(cols.Get(rec, isinNames...))}
		q, err := parser.ParseDecimal(cols.Get(rec, qtyNames...))
		if err != nil {
			issues = append(issues, parser.ParseIssue{Line: line, Reason: "quantity: " + err.Error(), Row: strings.Join(rec, ",")})
			continue
		}
		h.Quantity = q
		if p := cols.Get(rec, priceNames...); p != "" {
			if d, err := parser.ParseDecimal(p); err == nil {
				h.AvgPrice = d
			}
		}
		if h.ISIN == "" && h.Symbol == "" {
			issues = append(issues, parser.ParseIssue{Line: line, Reason: "no ISIN or symbol", Row: strings.Join(rec, ",")})
			continue
		}
		out = append(out, h)
	}
	return out, issues, nil
}

// Difference is one instrument where the broker and the engine disagree.
type Difference struct {
	ISIN           string          `json:"isin"`
	Symbol         string          `json:"symbol"`
	BrokerQuantity decimal.Decimal `json:"broker_quantity"`
	EngineQuantity decimal.Decimal `json:"engine_quantity"`
	Delta          decimal.Decimal `json:"delta"` // broker - engine
	BrokerAvgPrice decimal.Decimal `json:"broker_avg_price"`
	EngineAvgCost  decimal.Decimal `json:"engine_avg_cost"`
	Kind           string          `json:"kind"` // MISSING_HISTORY | EXTRA_IN_ENGINE | QUANTITY_MISMATCH | COST_MISMATCH
	Suggestion     string          `json:"suggestion"`
}

// Result of a reconciliation.
type Result struct {
	AccountID   string              `json:"account_id"`
	Matched     int                 `json:"matched"`
	Differences []Difference        `json:"differences"`
	Issues      []parser.ParseIssue `json:"issues"`
}

// Compare matches holdings to open lots of one account. symbolToISIN resolves statements that
// carry only symbols. Cost comparisons tolerate 1% because brokers round average prices.
func Compare(accountID string, holdings []Holding, lots []domain.Lot, symbolToISIN map[string]string) Result {
	res := Result{AccountID: accountID, Differences: []Difference{}}
	type agg struct {
		qty, cost decimal.Decimal
		symbol    string
	}
	engine := map[string]*agg{}
	for _, l := range lots {
		if l.AccountID != accountID || !l.QuantityOpen.IsPositive() {
			continue
		}
		a := engine[l.ISIN]
		if a == nil {
			a = &agg{symbol: l.Symbol}
			engine[l.ISIN] = a
		}
		a.qty = a.qty.Add(l.QuantityOpen)
		a.cost = a.cost.Add(l.CostOpen)
	}
	seen := map[string]bool{}
	for _, h := range holdings {
		isin := h.ISIN
		if isin == "" {
			isin = symbolToISIN[h.Symbol]
		}
		if isin == "" {
			res.Differences = append(res.Differences, Difference{Symbol: h.Symbol, BrokerQuantity: h.Quantity, Delta: h.Quantity, Kind: "MISSING_HISTORY",
				Suggestion: "No ISIN could be resolved for " + h.Symbol + "; add the ISIN mapping or upload the tradebook that includes it."})
			continue
		}
		seen[isin] = true
		a := engine[isin]
		if a == nil {
			res.Differences = append(res.Differences, Difference{ISIN: isin, Symbol: h.Symbol, BrokerQuantity: h.Quantity, Delta: h.Quantity, BrokerAvgPrice: h.AvgPrice, Kind: "MISSING_HISTORY",
				Suggestion: fmt.Sprintf("The broker holds %s units of %s but no purchase is on file. Upload the older tradebook, or enter an opening lot.", h.Quantity, h.Symbol)})
			continue
		}
		if !a.qty.Equal(h.Quantity) {
			kind := "QUANTITY_MISMATCH"
			if h.Quantity.GreaterThan(a.qty) {
				kind = "MISSING_HISTORY"
			}
			res.Differences = append(res.Differences, Difference{ISIN: isin, Symbol: h.Symbol, BrokerQuantity: h.Quantity, EngineQuantity: a.qty, Delta: h.Quantity.Sub(a.qty),
				BrokerAvgPrice: h.AvgPrice, EngineAvgCost: avg(a.cost, a.qty), Kind: kind,
				Suggestion: fmt.Sprintf("Broker shows %s, the engine computes %s (difference %s). Check for missing tradebook periods, transfers, splits or bonuses.", h.Quantity, a.qty, h.Quantity.Sub(a.qty))})
			continue
		}
		res.Matched++
		if h.AvgPrice.IsPositive() {
			e := avg(a.cost, a.qty)
			if e.IsPositive() && h.AvgPrice.Sub(e).Abs().Div(e).GreaterThan(decimal.NewFromFloat32(0.01)) { // nofloat:allow — 1% tolerance constant
				res.Differences = append(res.Differences, Difference{ISIN: isin, Symbol: h.Symbol, BrokerQuantity: h.Quantity, EngineQuantity: a.qty, BrokerAvgPrice: h.AvgPrice, EngineAvgCost: e, Kind: "COST_MISMATCH",
					Suggestion: fmt.Sprintf("Quantity matches but the broker's average price %s differs from the engine's %s. Brokers often show post-split or charge-inclusive averages; verify the lot costs.", h.AvgPrice.StringFixed(2), e.StringFixed(2))})
			}
		}
	}
	for isin, a := range engine {
		if !seen[isin] {
			res.Differences = append(res.Differences, Difference{ISIN: isin, Symbol: a.symbol, EngineQuantity: a.qty, Delta: a.qty.Neg(), EngineAvgCost: avg(a.cost, a.qty), Kind: "EXTRA_IN_ENGINE",
				Suggestion: fmt.Sprintf("The engine holds %s units of %s that the broker does not show. A sale, transfer-out or buyback may be missing from the uploaded tradebooks.", a.qty, a.symbol)})
		}
	}
	sort.SliceStable(res.Differences, func(i, j int) bool {
		if res.Differences[i].Kind != res.Differences[j].Kind {
			return res.Differences[i].Kind < res.Differences[j].Kind
		}
		return res.Differences[i].Symbol < res.Differences[j].Symbol
	})
	return res
}

func avg(cost, qty decimal.Decimal) decimal.Decimal {
	if !qty.IsPositive() {
		return decimal.Zero
	}
	return cost.Div(qty).Round(4)
}
