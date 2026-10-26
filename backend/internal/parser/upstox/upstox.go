// Package upstox parses the Upstox "Trade report" (trade book) export.
//
// Export: account.upstox.com > Reports > Trade (https://account.upstox.com/reports/trade), or in
// the app: Upstox icon > My account > Reports > Trade report. Pick the segment and a financial
// year or date range, "Get report", then download as Excel/XLS or CSV (the older Keystone back
// office "Export to CSV" trade book has the same shape). Upstox does not publish the header row
// and the equity and F&O reports differ, so columns are matched by name against a synonym list
// anchored on Upstox's own trade-history vocabulary
// (https://upstox.com/developer/api-documentation/get-historical-trades/):
//
//	Trade Date | Date,  Scrip Name | Symbol | Trading Symbol | Instrument,  ISIN,  Exchange,
//	Segment (EQ/FO/CD/COM/MF),  Trade Type | Buy/Sell | Side | Transaction Type (BUY/SELL),
//	Quantity | Qty,  Price | Trade Price,  Trade No | Trade Num | Trade ID,  Order No | Order ID,
//	Trade Time,  Expiry | Expiry Date,  Strike | Strike Price,  Option Type (CE/PE)
//
// Equity rows carry the ISIN (left empty when the export has none; the normaliser resolves it
// by symbol). F&O rows are typed from the Expiry/Strike/Option Type columns when present and
// otherwise decoded from the contract name ("NIFTY 27JUN24 23000 CE", "BANKNIFTY24JUNFUT").
// Legacy .xls workbooks must be re-saved as .xlsx or .csv. Menu paths:
// https://upstox.com/help-center/t-249884/ and
// https://help.mprofit.in/support/solutions/articles/82000889765.
package upstox

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Broker name used in the registry and account mapping.
const Broker = "upstox"

// Parser implements parser.Parser for Upstox trade reports.
type Parser struct{}

func init() { parser.Register(Parser{}) }

// Broker returns "upstox".
func (Parser) Broker() string { return Broker }

// colSet is the list of header spellings that mean the same column.
type colSet []string

func (s colSet) get(c parser.Columns, rec []string) string { return c.Get(rec, s...) }

func (s colSet) has(c parser.Columns) bool {
	for _, n := range s {
		if c.Has(n) {
			return true
		}
	}
	return false
}

var (
	colSymbol  = colSet{"scrip name", "script name", "symbol", "trading symbol", "tradingsymbol", "scrip", "instrument", "instrument name", "company", "company name"}
	colISIN    = colSet{"isin"}
	colExch    = colSet{"exchange", "exch"}
	colSeg     = colSet{"segment", "seg"}
	colDate    = colSet{"trade date", "date", "transaction date", "trade_date"}
	colSide    = colSet{"trade type", "buy/sell", "side", "transaction type", "b/s", "buy sell", "type"}
	colQty     = colSet{"quantity", "qty", "trade quantity", "traded quantity"}
	colPrice   = colSet{"price", "trade price", "rate", "avg price", "average price", "traded price"}
	colTradeID = colSet{"trade no", "trade no.", "trade num", "trade number", "trade id"}
	colOrderID = colSet{"order no", "order no.", "order num", "order number", "order id"}
	colTime    = colSet{"trade time", "time", "execution time"}
	colExpiry  = colSet{"expiry", "expiry date"}
	colStrike  = colSet{"strike", "strike price"}
	colOptType = colSet{"option type", "opt type", "ce/pe", "call/put"}
	colSeries  = colSet{"series"}

	requiredGroups = []colSet{colSymbol, colDate, colSide, colQty, colPrice}
)

type group struct {
	names  colSet
	weight float64
}

// detectGroups weight Upstox's space-separated spellings heavily and the generic columns
// (which Zerodha, Groww and Angel One share) lightly; the weights sum to 1.
var detectGroups = []group{
	{colSet{"scrip name", "script name"}, 0.12},
	{colSet{"trade date"}, 0.12},
	{colSet{"trade type", "side", "transaction type"}, 0.12},
	{colTradeID, 0.12},
	{colOrderID, 0.12},
	{colTime, 0.12},
	{colSet{"date"}, 0.04},
	{colSet{"buy/sell", "b/s", "type"}, 0.04},
	{colQty, 0.04},
	{colPrice, 0.04},
	{colExch, 0.04},
	{colSeg, 0.04},
	{colISIN, 0.04},
}

// Detect scores the header and every sample row (the Excel report may carry preamble rows).
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
	for _, g := range detectGroups {
		if g.names.has(c) {
			s += g.weight
		}
	}
	return s
}

func isHeader(cells []string) bool {
	c := parser.NewColumns(cells)
	for _, g := range requiredGroups {
		if !g.has(c) {
			return false
		}
	}
	return true
}

// Parse reads the XLSX or CSV export.
func (Parser) Parse(r io.Reader) ([]parser.RawTrade, []parser.ParseIssue, error) {
	rows, issues, err := readRows(r)
	if err != nil {
		return nil, nil, err
	}
	hi := -1
	for i := 0; i < len(rows) && i < headerScan; i++ {
		if isHeader(rows[i].cells) {
			hi = i
			break
		}
	}
	if hi < 0 {
		return nil, nil, errors.New("not an Upstox trade report: no header row with scrip name, trade date, trade type, quantity and price in the first 40 rows")
	}
	cols := parser.NewColumns(rows[hi].cells)
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
	t.Symbol = strings.ToUpper(colSymbol.get(c, rec))
	t.ISIN = strings.ToUpper(colISIN.get(c, rec))
	t.Exchange = strings.ToUpper(colExch.get(c, rec))
	t.Series = strings.ToUpper(colSeries.get(c, rec))
	t.TradeID = colTradeID.get(c, rec)
	t.OrderID = colOrderID.get(c, rec)
	if t.Symbol == "" {
		return t, errors.New("scrip name is empty")
	}
	var err error
	if t.Side, err = parser.ParseSide(colSide.get(c, rec)); err != nil {
		return t, err
	}
	if t.TradeDate, t.TradeTime, err = parseDateTime(colDate.get(c, rec)); err != nil {
		return t, fmt.Errorf("trade date: %w", err)
	}
	if clock := parseClock(colTime.get(c, rec)); clock != "" {
		t.TradeTime = clock
	}
	if t.Quantity, err = parser.ParseDecimal(colQty.get(c, rec)); err != nil || !t.Quantity.IsPositive() {
		return t, fmt.Errorf("quantity %q must be a positive number", colQty.get(c, rec))
	}
	if t.Price, err = parser.ParseDecimal(colPrice.get(c, rec)); err != nil || t.Price.IsNegative() {
		return t, fmt.Errorf("price %q must be a non-negative number", colPrice.get(c, rec))
	}
	return t, typeRow(&t, c, rec)
}

// typeRow sets the segment and, for derivatives, the contract fields.
func typeRow(t *parser.RawTrade, c parser.Columns, rec []string) error {
	expiry, strike, opt := colExpiry.get(c, rec), colStrike.get(c, rec), optToken(colOptType.get(c, rec))
	seg := colSeg.get(c, rec)
	switch segmentOf(seg) {
	case "EQ":
		t.Segment = domain.SegEquity
		return nil
	case "MF":
		t.Segment = domain.SegMutualFund
		return nil
	case "OTHER":
		t.Segment = domain.SegOther
		return nil
	case "FO":
	case "":
		if expiry == "" && opt == "" {
			if _, ok := decodeContract(t.Symbol); !ok || t.ISIN != "" {
				t.Segment = domain.SegEquity
				return nil
			}
		}
	default:
		return fmt.Errorf("unknown segment %q", seg)
	}
	return fnoRow(t, expiry, strike, opt)
}

// fnoRow types a derivative from the explicit columns, falling back to the contract name.
func fnoRow(t *parser.RawTrade, expiry, strike, opt string) error {
	if ct, ok := decodeContract(t.Symbol); ok {
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
	switch {
	case t.OptionType != "":
		t.Segment = domain.SegOption
		if !t.Strike.IsPositive() {
			return fmt.Errorf("strike is missing for option %q", t.Symbol)
		}
	case t.Segment != domain.SegOption:
		t.Segment = domain.SegFuture
		t.Strike, t.OptionType = t.Strike.Sub(t.Strike), ""
	}
	if t.Expiry.IsZero() {
		return fmt.Errorf("expiry is missing for F&O row %q", t.Symbol)
	}
	return nil
}

// segmentOf maps Upstox segment labels (EQ, FO, CD, COM, MF and spelled-out variants) to a
// class: "EQ", "MF", "FO", "OTHER", "" (absent) or "?" (unrecognised).
func segmentOf(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	switch {
	case s == "":
		return ""
	case s == "FO" || s == "NFO" || s == "BFO" || strings.HasSuffix(s, "_FO") || strings.Contains(s, "F&O") ||
		strings.Contains(s, "FNO") || strings.Contains(s, "DERIV") || strings.Contains(s, "FUT") || strings.Contains(s, "OPT"):
		return "FO"
	case s == "EQ" || s == "CM" || strings.HasSuffix(s, "_EQ") || strings.Contains(s, "EQUITY") || strings.Contains(s, "CASH"):
		return "EQ"
	case s == "MF" || strings.Contains(s, "MUTUAL"):
		return "MF"
	case s == "CD" || s == "CDS" || s == "COM" || s == "MCX" || s == "BCD" || strings.Contains(s, "CURR") || strings.Contains(s, "COMM"):
		return "OTHER"
	}
	return "?"
}
