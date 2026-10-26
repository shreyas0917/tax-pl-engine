package groww

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Build the byte order mark from bytes so the source stays clean.
var bom = string([]byte{0xEF, 0xBB, 0xBF})

const header = "Stock name,Symbol,ISIN,Type,Quantity,Value,Exchange,Exchange Order Id,Execution date and time,Order status\n"

var stocksCSV = bom + header +
	"Infosys Ltd,INFY,INE009A01021,BUY,10,14005,NSE,1100000000123456,13-09-2021 02:13 PM,Executed\n" +
	"Infosys Ltd,INFY,INE009A01021,SELL,10,\"15,000.00\",NSE,1100000000123457,10-02-2025 10:05 AM,Executed\n" +
	"\n" +
	"HDFC Bank Ltd,HDFCBANK,INE040A01034,BUY,2,3300,NSE,1100000000123459,04-06-2024 11:00 AM,Cancelled\n" +
	"Bad Co,BADCO,INE000000000,HOLD,2,3300,NSE,1100000000123460,04-06-2024 11:00 AM,Executed\n" +
	"No Qty,NOQTY,INE000000001,BUY,0,3300,NSE,1100000000123461,04-06-2024 11:00 AM,Executed\n" +
	"No Date,NODATE,INE000000002,BUY,1,3300,NSE,1100000000123462,yesterday,Executed\n"

func TestParseStocksCSV(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(stocksCSV))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, issues, 4, "cancelled and malformed rows are reported, never dropped")
	require.Equal(t, 5, issues[0].Line)
	require.Contains(t, issues[0].Reason, `order status "Cancelled"`)
	require.Contains(t, issues[1].Reason, "trade type")
	require.Contains(t, issues[2].Reason, "quantity")
	require.Contains(t, issues[3].Reason, "execution date and time")

	r := rows[0]
	require.Equal(t, 2, r.Line)
	require.Equal(t, "INFY", r.Symbol)
	require.Equal(t, "INE009A01021", r.ISIN)
	require.Equal(t, "NSE", r.Exchange)
	require.Equal(t, domain.Buy, r.Side)
	require.Equal(t, domain.SegEquity, r.Segment)
	require.Equal(t, domain.KindRegular, r.Kind)
	require.Equal(t, "10", r.Quantity.String())
	require.Equal(t, "1400.5", r.Price.String(), "price is Value / Quantity")
	require.Equal(t, "2021-09-13", r.TradeDate.String())
	require.Equal(t, "14:13:00", r.TradeTime)
	require.Equal(t, "1100000000123456", r.OrderID)
	require.Equal(t, "1100000000123456", r.TradeID)
	require.Equal(t, "Infosys Ltd", r.Raw["stock name"])
	require.Equal(t, "1500", rows[1].Price.String(), "thousands separators inside quotes are handled")
	require.Equal(t, domain.Sell, rows[1].Side)
}

func TestParseXLSXWithPreamble(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	set := func(cell string, vals ...interface{}) { require.NoError(t, f.SetSheetRow(sheet, cell, &vals)) }
	set("A1", "Name", "Demo User")
	set("A2", "Unique Client Code", "0000000000")
	set("A4", "Order history for stocks from 01-01-2020 to 01-01-2026")
	set("A6", "Stock name", "Symbol", "ISIN", "Type", "Quantity", "Value", "Exchange", "Exchange Order Id", "Execution date and time", "Order status")
	set("A7", "Infosys Ltd", "INFY", "INE009A01021", "BUY", 10, 14005, "NSE", "ORD1", "13-09-2021 02:13 PM", "Executed")
	// A real date cell arrives as an Excel serial number in raw mode.
	set("A8", "Infosys Ltd", "INFY", "INE009A01021", "SELL", 4, 6000, "NSE", "ORD2", time.Date(2025, 2, 10, 10, 5, 0, 0, time.UTC), "Executed")
	set("A9", "Nifty 50", "NIFTY 27JUN24 23000 CE", "", "BUY", 50, 5000, "NSE", "ORD3", "03-06-2024 09:30 AM", "Executed")
	set("A10", "Zero Co", "ZERO", "INE000000003", "BUY", 1, 10, "NSE", "ORD4", "03-06-2024 09:30 AM", "Rejected")
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)

	rows, issues, err := Parser{}.Parse(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Len(t, issues, 1)
	require.Equal(t, 10, issues[0].Line, "sheet row numbers are reported")
	require.Equal(t, 7, rows[0].Line)
	require.Equal(t, "1400.5", rows[0].Price.String())
	require.Equal(t, "2025-02-10", rows[1].TradeDate.String())
	require.Equal(t, "10:05:00", rows[1].TradeTime)
	require.Equal(t, "1500", rows[1].Price.String())

	opt := rows[2]
	require.Equal(t, domain.SegOption, opt.Segment)
	require.Equal(t, "NIFTY", opt.Symbol)
	require.Equal(t, "2024-06-27", opt.Expiry.String())
	require.Equal(t, "23000", opt.Strike.String())
	require.Equal(t, "CE", opt.OptionType)
}

func TestDecodeContract(t *testing.T) {
	cases := map[string]struct {
		und, expiry, strike, typ string
		seg                      domain.Segment
	}{
		"NIFTY 27JUN24 23000 CE":           {"NIFTY", "2024-06-27", "23000", "CE", domain.SegOption},
		"NIFTY27JUN2423000CE":              {"NIFTY", "2024-06-27", "23000", "CE", domain.SegOption},
		"BANKNIFTY 26-JUN-2024 PE 48500.5": {"BANKNIFTY", "2024-06-26", "48500.5", "PE", domain.SegOption},
		"BAJAJ-AUTO 27 JUN 2024 FUT":       {"BAJAJ-AUTO", "2024-06-27", "0", "", domain.SegFuture},
		"M&M 25JUL24 FUT":                  {"M&M", "2024-07-25", "0", "", domain.SegFuture},
		"NIFTY24JUN23000CE":                {"NIFTY", "0000-00-00", "23000", "CE", domain.SegOption},
		"BANKNIFTY24JUNFUT":                {"BANKNIFTY", "0000-00-00", "0", "", domain.SegFuture},
	}
	for sym, want := range cases {
		c, ok := decodeContract(sym)
		require.True(t, ok, sym)
		require.Equal(t, want.und, c.underlying, sym)
		require.Equal(t, want.expiry, c.expiry.String(), sym)
		require.Equal(t, want.strike, c.strike.String(), sym)
		require.Equal(t, want.typ, c.optType, sym)
		require.Equal(t, want.seg, c.segment, sym)
	}
	for _, sym := range []string{"INFY", "JUNIPER HOTELS", "3M INDIA", "SUN PHARMA 12 MAY", "NIFTY 27JUN24 0 CE"} {
		_, ok := decodeContract(sym)
		require.False(t, ok, sym)
	}
}

func TestDetect(t *testing.T) {
	hdr, sample, err := parser.ReadCSVHead(strings.NewReader(stocksCSV), 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(hdr, sample), 0.9)

	zerodha := []string{"symbol", "isin", "trade_date", "exchange", "segment", "series", "trade_type", "auction", "quantity", "price", "trade_id", "order_id", "order_execution_time"}
	upstox := []string{"trade date", "scrip name", "isin", "exchange", "segment", "trade type", "quantity", "price", "trade no", "order no", "trade time", "expiry", "strike", "option type"}
	angel := []string{"scrip/contract", "buy/sell", "buy price", "sell price", "quantity", "brokerage", "order type", "segment", "date"}
	for _, foreign := range [][]string{zerodha, upstox, angel, {"date", "scrip", "qty"}} {
		require.Less(t, Parser{}.Detect(foreign, nil), 0.3, foreign)
	}

	// Preamble rows push the header into the sample; Detect still finds it.
	preamble := "Name,Demo User\nUnique Client Code,0000000000\n\nOrder history for stocks from 01-01-2020 to 01-01-2026\n\n" + stocksCSV[len(bom):]
	hdr, sample, err = parser.ReadCSVHead(strings.NewReader(preamble), 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(hdr, sample), 0.9)

	p, det, ok := parser.Best([]byte(stocksCSV))
	require.True(t, ok)
	require.Equal(t, Broker, det.Broker)
	require.Equal(t, Broker, p.Broker())
}

func TestSampleFile(t *testing.T) {
	data, err := os.ReadFile("testdata/sample.csv")
	require.NoError(t, err)
	p, _, ok := parser.Best(data)
	require.True(t, ok)
	require.Equal(t, Broker, p.Broker())
	rows, issues, err := p.Parse(bytes.NewReader(data))
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Len(t, issues, 1)
	require.Equal(t, 5, issues[0].Line)
	require.Equal(t, "650.1", rows[2].Price.String())
	require.Equal(t, "BSE", rows[2].Exchange)
}

func TestNotGroww(t *testing.T) {
	_, _, err := Parser{}.Parse(strings.NewReader("a,b,c\n1,2,3\n"))
	require.Error(t, err)
	_, _, err = Parser{}.Parse(strings.NewReader(""))
	require.Error(t, err)
}
