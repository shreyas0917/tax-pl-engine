// Package groww parses the Groww "Stocks Order History" export.
//
// Export: Groww app or web > Profile > Reports > Transactions (transaction report) > Order
// history (stocks) > pick a date range > Download. The file is an XLSX workbook (older exports
// are password protected with the PAN in capitals; save an unprotected copy first). The first
// sheet starts with a few preamble rows (Name, Unique Client Code, "Order history for stocks
// from dd-mm-yyyy to dd-mm-yyyy") and then the header:
//
//	Stock name, Symbol, ISIN, Type, Quantity, Value, Exchange, Exchange Order Id,
//	Execution date and time, Order status
//
// Type is BUY/SELL, Value is the total order value (price = Value / Quantity; a Price column
// is used instead when present), "Execution date and time" looks like "13-09-2021 02:13 PM"
// and Order status is "Executed" for filled orders. Rows with any other status are reported as
// issues, never dropped. The same columns are accepted as CSV.
//
// Sources: https://github.com/ash4691-rgb/finSights/pull/114 (fixture mirroring a real export),
// https://github.com/vinitgrwl/PortfolioTracker/blob/main/lib/parsers/groww.ts (parser of the
// same export), https://www.tradesviz.com/how-to-export-trades/groww/ (menu path, XLSX) and
// https://groww.in/help/my-account/ma-others/where-can-i-get-the-transaction-history (password).
package groww

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Broker name used in the registry and account mapping.
const Broker = "groww"

// Parser implements parser.Parser for Groww stocks order history exports.
type Parser struct{}

func init() { parser.Register(Parser{}) }

// Broker returns "groww".
func (Parser) Broker() string { return Broker }

var required = []string{"stock name", "symbol", "isin", "type", "quantity", "execution date and time", "order status"}

// weights score the distinctive Groww column set; they sum to 1.
var weights = map[string]float64{
	"stock name": 0.2, "execution date and time": 0.2, "order status": 0.2,
	"exchange order id": 0.1, "value": 0.1,
	"isin": 0.05, "type": 0.05, "symbol": 0.05, "quantity": 0.05,
}

// Detect scores the header and, because the real export has preamble rows, every sample row.
func (Parser) Detect(header []string, sample [][]string) float64 {
	best := score(header)
	for _, r := range sample {
		if s := score(r); s > best {
			best = s
		}
	}
	return best
}

func score(h []string) float64 {
	c := parser.NewColumns(h)
	s := 0.0
	for name, w := range weights {
		if c.Has(name) {
			s += w
		}
	}
	return s
}

// Parse reads the XLSX or CSV export.
func (Parser) Parse(r io.Reader) ([]parser.RawTrade, []parser.ParseIssue, error) {
	rows, issues, err := readRows(r)
	if err != nil {
		return nil, nil, err
	}
	hi := findHeader(rows, required)
	if hi < 0 {
		return nil, nil, fmt.Errorf("not a Groww stocks order history: no header row with columns %q in the first %d rows", required, headerScan)
	}
	cols := parser.NewColumns(rows[hi].cells)
	if !cols.Has("value") && !cols.Has("price") {
		return nil, nil, errors.New("not a Groww stocks order history: neither a Value nor a Price column")
	}
	var trades []parser.RawTrade
	for _, rw := range rows[hi+1:] {
		if isBlank(rw.cells) {
			continue
		}
		t, perr := parseRow(cols, rw.cells, rw.line)
		if perr != nil {
			issues = append(issues, parser.ParseIssue{Line: rw.line, Reason: perr.Error(), Row: strings.Join(rw.cells, ",")})
			continue
		}
		trades = append(trades, t)
	}
	return trades, issues, nil
}

func parseRow(c parser.Columns, rec []string, line int) (parser.RawTrade, error) {
	t := parser.RawTrade{Line: line, Broker: Broker, Kind: domain.KindRegular, Raw: map[string]string{}}
	for name, i := range c {
		if i < len(rec) {
			t.Raw[name] = rec[i]
		}
	}
	t.Symbol = strings.ToUpper(c.Get(rec, "symbol"))
	t.ISIN = strings.ToUpper(c.Get(rec, "isin"))
	t.Exchange = strings.ToUpper(c.Get(rec, "exchange"))
	t.OrderID = c.Get(rec, "exchange order id", "order id")
	t.TradeID = t.OrderID // Groww prints no trade number; the exchange order id is the stable key
	if t.Symbol == "" {
		return t, errors.New("symbol is empty")
	}
	if status := c.Get(rec, "order status"); !strings.EqualFold(status, "executed") {
		return t, fmt.Errorf("order status %q is not an executed trade", status)
	}
	var err error
	if t.Side, err = parser.ParseSide(c.Get(rec, "type")); err != nil {
		return t, err
	}
	if t.TradeDate, t.TradeTime, err = parseDateTime(c.Get(rec, "execution date and time")); err != nil {
		return t, fmt.Errorf("execution date and time: %w", err)
	}
	if t.Quantity, err = parser.ParseDecimal(c.Get(rec, "quantity")); err != nil || !t.Quantity.IsPositive() {
		return t, fmt.Errorf("quantity %q must be a positive number", c.Get(rec, "quantity"))
	}
	if p := c.Get(rec, "price"); p != "" {
		if t.Price, err = parser.ParseDecimal(p); err != nil || t.Price.IsNegative() {
			return t, fmt.Errorf("price %q must be a non-negative number", p)
		}
	} else {
		value, verr := parser.ParseDecimal(c.Get(rec, "value"))
		if verr != nil || value.IsNegative() {
			return t, fmt.Errorf("value %q must be a non-negative number", c.Get(rec, "value"))
		}
		t.Price = value.Div(t.Quantity)
	}
	t.Segment = domain.SegEquity
	if t.ISIN == "" {
		// The stocks report is equity only, but a contract-shaped symbol without an ISIN is F&O.
		if ct, ok := decodeContract(t.Symbol); ok {
			if ct.expiry.IsZero() {
				return t, fmt.Errorf("cannot determine expiry of F&O symbol %q", t.Symbol)
			}
			t.Symbol, t.Segment, t.Expiry, t.Strike, t.OptionType = ct.underlying, ct.segment, ct.expiry, ct.strike, ct.optType
		}
	}
	return t, nil
}
