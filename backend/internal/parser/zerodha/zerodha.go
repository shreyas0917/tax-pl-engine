// Package zerodha parses Zerodha Console tradebook exports (equity and F&O CSV).
//
// Equity tradebook columns (Console > Reports > Tradebook > CSV):
//
//	symbol,isin,trade_date,exchange,segment,series,trade_type,auction,quantity,price,trade_id,order_id,order_execution_time
//
// F&O tradebook adds expiry_date and leaves isin empty; the symbol encodes the contract
// (NIFTY24APR22500CE, BANKNIFTY24MAYFUT). Rows that cannot be typed become ParseIssues.
package zerodha

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Broker name used in the registry and account mapping.
const Broker = "zerodha"

// Parser implements parser.Parser for Zerodha Console tradebooks.
type Parser struct{}

func init() { parser.Register(Parser{}) }

// Broker returns "zerodha".
func (Parser) Broker() string { return Broker }

var required = []string{"symbol", "isin", "trade_date", "exchange", "segment", "trade_type", "quantity", "price", "trade_id", "order_id"}

// Detect scores by the presence of Zerodha's distinctive column set.
func (Parser) Detect(header []string, sample [][]string) float64 {
	c := parser.NewColumns(header)
	score := 0.0
	for _, r := range required {
		if c.Has(r) {
			score += 1.0 / float64(len(required))
		}
	}
	if c.Has("order_execution_time") {
		score = score*0.9 + 0.1
	}
	if c.Has("auction") {
		score = score*0.9 + 0.1
	}
	return score
}

var (
	optionRe = regexp.MustCompile(`^([A-Z&\-]+?)(\d{2}[A-Z0-9]{3})(\d+(?:\.\d+)?)(CE|PE)$`)
	futureRe = regexp.MustCompile(`^([A-Z&\-]+?)(\d{2}[A-Z]{3})FUT$`)
)

// Parse reads the CSV.
func (Parser) Parse(r io.Reader) ([]parser.RawTrade, []parser.ParseIssue, error) {
	cr := parser.NewCSVReader(r)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read header: %w", err)
	}
	cols := parser.NewColumns(header)
	for _, req := range required {
		if !cols.Has(req) {
			return nil, nil, fmt.Errorf("not a Zerodha tradebook: missing column %q", req)
		}
	}
	var (
		trades []parser.RawTrade
		issues []parser.ParseIssue
	)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			line := 0
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				line = pe.Line
			}
			issues = append(issues, parser.ParseIssue{Line: line, Reason: "malformed CSV row: " + err.Error()})
			continue
		}
		if isBlank(rec) {
			continue
		}
		line, _ := cr.FieldPos(0) // real source line, blank lines included
		t, perr := parseRow(cols, rec, line)
		if perr != nil {
			issues = append(issues, parser.ParseIssue{Line: line, Reason: perr.Error(), Row: strings.Join(rec, ",")})
			continue
		}
		trades = append(trades, t)
	}
	return trades, issues, nil
}

func isBlank(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
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
	t.Series = strings.ToUpper(c.Get(rec, "series"))
	t.TradeID = c.Get(rec, "trade_id")
	t.OrderID = c.Get(rec, "order_id")
	if t.Symbol == "" {
		return t, fmt.Errorf("symbol is empty")
	}
	var err error
	if t.Side, err = parser.ParseSide(c.Get(rec, "trade_type")); err != nil {
		return t, err
	}
	if t.TradeDate, err = domain.ParseDate(c.Get(rec, "trade_date")); err != nil {
		return t, fmt.Errorf("trade_date: %w", err)
	}
	if t.Quantity, err = parser.ParseDecimal(c.Get(rec, "quantity")); err != nil || !t.Quantity.IsPositive() {
		return t, fmt.Errorf("quantity %q must be a positive number", c.Get(rec, "quantity"))
	}
	if t.Price, err = parser.ParseDecimal(c.Get(rec, "price")); err != nil || t.Price.IsNegative() {
		return t, fmt.Errorf("price %q must be a non-negative number", c.Get(rec, "price"))
	}
	if ts := c.Get(rec, "order_execution_time"); ts != "" {
		t.TradeTime = extractTime(ts)
	}
	seg := strings.ToUpper(c.Get(rec, "segment"))
	switch seg {
	case "EQ", "":
		t.Segment = domain.SegEquity
		if t.ISIN == "" {
			return t, fmt.Errorf("isin is empty for equity row")
		}
	case "FO", "F&O", "NFO", "BFO":
		if err := parseContract(&t, c.Get(rec, "expiry_date")); err != nil {
			return t, err
		}
	case "MF":
		t.Segment = domain.SegMutualFund
	case "CDS", "MCX", "COM", "CO":
		t.Segment = domain.SegOther
	default:
		t.Segment = domain.SegOther
	}
	return t, nil
}

func extractTime(ts string) string {
	ts = strings.TrimSpace(ts)
	if i := strings.IndexAny(ts, "T "); i >= 0 && len(ts) > i+1 {
		ts = ts[i+1:]
	}
	if len(ts) >= 8 {
		return ts[:8]
	}
	return ts
}

// parseContract decodes the Zerodha F&O symbol into underlying, expiry, strike and type.
func parseContract(t *parser.RawTrade, expiry string) error {
	if m := optionRe.FindStringSubmatch(t.Symbol); m != nil {
		t.Segment = domain.SegOption
		strike, err := decimal.NewFromString(m[3])
		if err != nil {
			return fmt.Errorf("strike in symbol %q: %w", t.Symbol, err)
		}
		t.Strike = strike
		t.OptionType = m[4]
		t.Symbol = m[1]
	} else if m := futureRe.FindStringSubmatch(t.Symbol); m != nil {
		t.Segment = domain.SegFuture
		t.Symbol = m[1]
	} else {
		return fmt.Errorf("cannot decode F&O symbol %q", t.Symbol)
	}
	if expiry == "" {
		return fmt.Errorf("expiry_date is empty for F&O row")
	}
	d, err := domain.ParseDate(expiry)
	if err != nil {
		return fmt.Errorf("expiry_date: %w", err)
	}
	t.Expiry = d
	return nil
}
