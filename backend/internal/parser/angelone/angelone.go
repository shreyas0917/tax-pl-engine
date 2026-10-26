// Package angelone parses the Angel One "Trade history" (Trades and charges) export.
//
// Export: Angel One app > Account > Trades and charges > pick a date range > Download trade
// history (the web back office calls the same report "Trade History / Sauda Summary"). The
// workbook has a sheet named TradesAndCharges with a short preamble (ClientCode, ...) followed
// by the header:
//
//	Scrip/Contract, Buy/Sell, Buy Price, Sell Price, Quantity, Brokerage, Order Type, Segment,
//	Date, GST, STT, SEBI Tax, Exchange Turnover Charges, Stamp Duty, Other Charges, IPFT charges
//
// There is no ISIN column: equity rows leave ISIN empty and the normaliser resolves it by
// symbol. Segment is "Capital" for cash equity; equity and F&O come in one file and F&O rows
// carry the contract in Scrip/Contract ("NIFTY 27JUN24 23000 CE", "BANKNIFTY 26JUN2024 FUT"),
// which is decoded, or typed from Expiry Date / Strike Price / Option Type columns when the
// export has them. Order Type (Delivery/Intraday/...) is kept in Raw only; intraday pairing is
// the engine's job. The charge columns are imported into Charges with Source "imported" (IPFT
// and Other Charges are folded into ExchangeFee). The same columns are accepted as CSV; a
// legacy .xls must be re-saved as .xlsx or .csv.
//
// Sources: https://www.angelone.in/support/reports-and-statements/trade-history-sauda-summary
// (menu path), https://github.com/vinitgrwl/PortfolioTracker/blob/main/lib/parsers/angelone.ts
// (sheet and column names of a real export), https://arthveda.app/brokers/angel-one (XLSX/XLS/
// CSV, equity and F&O together) and https://learn.quicko.com/download-trade-book-angel-broking.
package angelone

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Broker name used in the registry and account mapping.
const Broker = "angelone"

// Parser implements parser.Parser for Angel One trade history exports.
type Parser struct{}

func init() { parser.Register(Parser{}) }

// Broker returns "angelone".
func (Parser) Broker() string { return Broker }

var required = []string{"scrip/contract", "buy/sell", "quantity", "date"}

// weights score the distinctive Angel One column set; they sum to 1 with the charges bonus.
var weights = map[string]float64{
	"scrip/contract": 0.25, "buy price": 0.15, "sell price": 0.15, "buy/sell": 0.1, "order type": 0.1,
	"brokerage": 0.05, "segment": 0.05, "date": 0.05, "quantity": 0.05,
}

var chargeHeaders = []string{"stt", "gst", "stamp duty", "sebi tax"}

// Detect scores the header and every sample row (the real export has preamble rows).
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
	for _, name := range chargeHeaders {
		if c.Has(name) {
			s += 0.05
			break
		}
	}
	return s
}

// Parse reads the XLSX (TradesAndCharges sheet) or CSV export.
func (Parser) Parse(r io.Reader) ([]parser.RawTrade, []parser.ParseIssue, error) {
	rows, issues, err := readRows(r)
	if err != nil {
		return nil, nil, err
	}
	hi := findHeader(rows, required)
	if hi < 0 {
		return nil, nil, fmt.Errorf("not an Angel One trade history: no header row with columns %q in the first %d rows", required, headerScan)
	}
	cols := parser.NewColumns(rows[hi].cells)
	if !cols.Has("buy price", "sell price") && !cols.Has("price") && !cols.Has("trade price") {
		return nil, nil, errors.New("not an Angel One trade history: no Buy Price/Sell Price or Price column")
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
	t.Symbol = strings.ToUpper(c.Get(rec, "scrip/contract"))
	t.ISIN = strings.ToUpper(c.Get(rec, "isin"))
	t.Exchange = strings.ToUpper(c.Get(rec, "exchange", "exch"))
	t.TradeID = c.Get(rec, "trade no", "trade no.", "trade number", "trade id")
	t.OrderID = c.Get(rec, "order no", "order no.", "order number", "order id")
	if t.Symbol == "" {
		return t, errors.New("scrip/contract is empty")
	}
	var err error
	if t.Side, err = parser.ParseSide(c.Get(rec, "buy/sell")); err != nil {
		return t, err
	}
	if t.TradeDate, t.TradeTime, err = parseDateTime(c.Get(rec, "date")); err != nil {
		return t, fmt.Errorf("date: %w", err)
	}
	if clock := parseClock(c.Get(rec, "trade time", "time")); clock != "" {
		t.TradeTime = clock
	}
	if t.Quantity, err = parser.ParseDecimal(c.Get(rec, "quantity")); err != nil || !t.Quantity.IsPositive() {
		return t, fmt.Errorf("quantity %q must be a positive number", c.Get(rec, "quantity"))
	}
	priceCol := "buy price"
	if t.Side == domain.Sell {
		priceCol = "sell price"
	}
	price := c.Get(rec, priceCol, "price", "trade price")
	if t.Price, err = parser.ParseDecimal(price); err != nil || t.Price.IsNegative() {
		return t, fmt.Errorf("%s %q must be a non-negative number", priceCol, price)
	}
	if t.Charges, err = parseCharges(c, rec); err != nil {
		return t, err
	}
	return t, typeRow(&t, c, rec)
}

// chargeCols maps Angel One charge headers to the Charges field they feed.
var chargeCols = []struct {
	name  string
	apply func(ch *domain.Charges, v decimal.Decimal)
}{
	{"brokerage", func(ch *domain.Charges, v decimal.Decimal) { ch.Brokerage = ch.Brokerage.Add(v) }},
	{"gst", func(ch *domain.Charges, v decimal.Decimal) { ch.GST = ch.GST.Add(v) }},
	{"stt", func(ch *domain.Charges, v decimal.Decimal) { ch.STT = ch.STT.Add(v) }},
	{"sebi tax", func(ch *domain.Charges, v decimal.Decimal) { ch.SEBIFee = ch.SEBIFee.Add(v) }},
	{"sebi fee", func(ch *domain.Charges, v decimal.Decimal) { ch.SEBIFee = ch.SEBIFee.Add(v) }},
	{"exchange turnover charges", func(ch *domain.Charges, v decimal.Decimal) { ch.ExchangeFee = ch.ExchangeFee.Add(v) }},
	{"transaction charges", func(ch *domain.Charges, v decimal.Decimal) { ch.ExchangeFee = ch.ExchangeFee.Add(v) }},
	{"ipft charges", func(ch *domain.Charges, v decimal.Decimal) { ch.ExchangeFee = ch.ExchangeFee.Add(v) }},
	{"other charges", func(ch *domain.Charges, v decimal.Decimal) { ch.ExchangeFee = ch.ExchangeFee.Add(v) }},
	{"stamp duty", func(ch *domain.Charges, v decimal.Decimal) { ch.StampDuty = ch.StampDuty.Add(v) }},
}

// parseCharges imports the charge columns; nil when the export has none. Charges are printed
// as positive amounts or as debits, so the absolute value is taken.
func parseCharges(c parser.Columns, rec []string) (*domain.Charges, error) {
	var ch *domain.Charges
	for _, col := range chargeCols {
		if !c.Has(col.name) {
			continue
		}
		if ch == nil {
			ch = &domain.Charges{Source: "imported"}
		}
		v := c.Get(rec, col.name)
		if v == "" || v == "-" {
			continue
		}
		d, err := parser.ParseDecimal(v)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", col.name, v, err)
		}
		col.apply(ch, d.Abs())
	}
	return ch, nil
}

// typeRow sets the segment and, for derivatives, the contract fields.
func typeRow(t *parser.RawTrade, c parser.Columns, rec []string) error {
	expiry := c.Get(rec, "expiry date", "expiry")
	strike := c.Get(rec, "strike price", "strike")
	opt := optToken(c.Get(rec, "option type", "opt type"))
	seg := c.Get(rec, "segment")
	switch segmentOf(seg) {
	case "EQ":
		t.Segment = domain.SegEquity
		return nil
	case "OTHER":
		t.Segment = domain.SegOther
		return nil
	case "FO":
	case "":
		if expiry == "" && opt == "" {
			if _, ok := decodeContract(t.Symbol); !ok {
				t.Segment = domain.SegEquity
				return nil
			}
		}
	default:
		return fmt.Errorf("unknown segment %q", seg)
	}
	return fnoRow(t, expiry, strike, opt)
}

// fnoRow types a derivative from the contract name, overridden by explicit columns.
func fnoRow(t *parser.RawTrade, expiry, strike, opt string) error {
	ct, decoded := decodeContract(t.Symbol)
	if decoded {
		t.Symbol, t.Segment, t.Expiry, t.Strike, t.OptionType = ct.underlying, ct.segment, ct.expiry, ct.strike, ct.optType
	}
	if expiry != "" {
		d, _, err := parseDateTime(expiry)
		if err != nil {
			return fmt.Errorf("expiry: %w", err)
		}
		t.Expiry = d
	}
	if opt != "" {
		t.OptionType = opt
	}
	if strike != "" {
		s, err := parser.ParseDecimal(strike)
		if err != nil {
			return fmt.Errorf("strike %q: %w", strike, err)
		}
		if s.IsPositive() {
			t.Strike = s
		}
	}
	if !decoded && opt == "" && expiry == "" {
		return fmt.Errorf("cannot decode F&O contract %q", t.Symbol)
	}
	switch {
	case t.OptionType != "":
		t.Segment = domain.SegOption
		if !t.Strike.IsPositive() {
			return fmt.Errorf("strike is missing for option %q", t.Symbol)
		}
	case t.Segment != domain.SegOption:
		t.Segment = domain.SegFuture
		t.Strike, t.OptionType = decimal.Zero, ""
	}
	if t.Expiry.IsZero() {
		return fmt.Errorf("expiry is missing for F&O row %q", t.Symbol)
	}
	return nil
}

// segmentOf maps Angel One segment labels to a class: "EQ", "FO", "OTHER", "" (absent) or
// "?" (unrecognised). "Capital" is what the real export prints for cash equity.
func segmentOf(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case s == "":
		return ""
	case s == "FO" || s == "NFO" || s == "BFO" || strings.Contains(s, "F&O") || strings.Contains(s, "FNO") ||
		strings.Contains(s, "DERIV") || strings.Contains(s, "FUT") || strings.Contains(s, "OPT"):
		return "FO"
	case s == "EQ" || s == "CM" || strings.Contains(s, "CAPITAL") || strings.Contains(s, "CASH") || strings.Contains(s, "EQUITY"):
		return "EQ"
	case s == "CD" || s == "CDS" || s == "COM" || s == "MCX" || strings.Contains(s, "CURR") || strings.Contains(s, "COMM"):
		return "OTHER"
	}
	return "?"
}
