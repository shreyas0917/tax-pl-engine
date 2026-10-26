package upstox

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Build the byte order mark from bytes so the source stays clean.
var bom = string([]byte{0xEF, 0xBB, 0xBF})

const header = "Trade Date,Scrip Name,ISIN,Exchange,Segment,Trade Type,Quantity,Price,Trade No,Order No,Trade Time,Expiry,Strike,Option Type\n"

var tradeCSV = bom + header +
	"2024-06-03,RELIANCE,INE002A01018,NSE,EQ,BUY,10,2252.35,75217259,1100000000000001,10:15:22,,0.0,\n" +
	"10-02-2025,RELIANCE,INE002A01018,NSE,EQ,SELL,10,\"2,900.00\",75217260,1100000000000002,14:01:02,,0.0,\n" +
	"\n" +
	"2024-06-03,BANKNIFTY,,NFO,FO,BUY,15,609.5,430927903,1100000000000003,09:30:00,2024-06-26,40000.0,CE\n" +
	"2024-06-03,NIFTY,,NFO,FO,SELL,25,150.25,430927904,1100000000000004,09:31:00,2024-06-27,0.0,\n" +
	"2024-06-04,NIFTY,,NFO,FO,SELL,25,150.25,430927905,1100000000000005,09:31:00,,0.0,\n" +
	"2024-06-04,TCS,INE467B01029,NSE,EQ,HOLD,1,100,75217261,1100000000000006,09:32:00,,0.0,\n" +
	"2024-06-04,TCS,INE467B01029,NSE,EQ,BUY,-1,100,75217262,1100000000000007,09:32:00,,0.0,\n" +
	"2024-06-04,TCS,INE467B01029,NSE,XYZ,BUY,1,100,75217263,1100000000000008,09:32:00,,0.0,\n"

func TestParse(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(tradeCSV))
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Len(t, issues, 4, "every bad row is reported, none dropped silently")
	require.Equal(t, 7, issues[0].Line)
	require.Contains(t, issues[0].Reason, "expiry is missing")
	require.Contains(t, issues[1].Reason, "trade type")
	require.Contains(t, issues[2].Reason, "quantity")
	require.Contains(t, issues[3].Reason, `unknown segment "XYZ"`)

	eq := rows[0]
	require.Equal(t, 2, eq.Line)
	require.Equal(t, "RELIANCE", eq.Symbol)
	require.Equal(t, "INE002A01018", eq.ISIN)
	require.Equal(t, "NSE", eq.Exchange)
	require.Equal(t, domain.SegEquity, eq.Segment)
	require.Equal(t, domain.KindRegular, eq.Kind)
	require.Equal(t, domain.Buy, eq.Side)
	require.Equal(t, "10", eq.Quantity.String())
	require.Equal(t, "2252.35", eq.Price.String())
	require.Equal(t, "2024-06-03", eq.TradeDate.String())
	require.Equal(t, "10:15:22", eq.TradeTime)
	require.Equal(t, "75217259", eq.TradeID)
	require.Equal(t, "1100000000000001", eq.OrderID)
	require.Equal(t, "RELIANCE", eq.Raw["scrip name"])
	require.Equal(t, "2025-02-10", rows[1].TradeDate.String(), "dd-mm-yyyy dates are accepted")
	require.Equal(t, "2900", rows[1].Price.String())

	opt := rows[2]
	require.Equal(t, domain.SegOption, opt.Segment)
	require.Equal(t, "BANKNIFTY", opt.Symbol)
	require.Equal(t, "2024-06-26", opt.Expiry.String())
	require.Equal(t, "40000", opt.Strike.String())
	require.Equal(t, "CE", opt.OptionType)
	require.Equal(t, "NFO", opt.Exchange)

	fut := rows[3]
	require.Equal(t, domain.SegFuture, fut.Segment)
	require.Equal(t, "NIFTY", fut.Symbol)
	require.Equal(t, "2024-06-27", fut.Expiry.String())
	require.True(t, fut.Strike.IsZero())
	require.Equal(t, "", fut.OptionType)
	require.Equal(t, domain.Sell, fut.Side)
}

// A leaner report without contract columns: the contract is decoded from the name.
const leanCSV = "Date,Symbol,Buy/Sell,Qty,Trade Price,Segment\n" +
	"03-06-2024 09:30:00,NIFTY 27JUN24 23000 CE,Buy,50,100,FO\n" +
	"03-06-2024,BANKNIFTY26JUN2024FUT,Sell,15,50000,F&O\n" +
	"03-06-2024,INFY,Buy,10,1400.5,\n" +
	"03-06-2024,NIFTY 27JUN24 23000 CE,Buy,50,100,\n" +
	"03-06-2024,WEIRD,Buy,15,10,FO\n"

func TestParseDecodesContractNames(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(leanCSV))
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Len(t, issues, 1)
	require.Equal(t, 6, issues[0].Line)
	require.Contains(t, issues[0].Reason, "expiry is missing")

	require.Equal(t, domain.SegOption, rows[0].Segment)
	require.Equal(t, "NIFTY", rows[0].Symbol)
	require.Equal(t, "23000", rows[0].Strike.String())
	require.Equal(t, "CE", rows[0].OptionType)
	require.Equal(t, "2024-06-27", rows[0].Expiry.String())
	require.Equal(t, "09:30:00", rows[0].TradeTime)
	require.Equal(t, domain.SegFuture, rows[1].Segment)
	require.Equal(t, "BANKNIFTY", rows[1].Symbol)
	require.Equal(t, "2024-06-26", rows[1].Expiry.String())
	require.Equal(t, domain.SegEquity, rows[2].Segment, "no segment, no contract shape: equity with empty ISIN")
	require.Equal(t, "", rows[2].ISIN)
	require.Equal(t, domain.SegOption, rows[3].Segment, "no segment but a contract-shaped name is F&O")
}

func TestParseXLSX(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	set := func(cell string, vals ...interface{}) { require.NoError(t, f.SetSheetRow(sheet, cell, &vals)) }
	set("A1", "Trade report for FY 2024-25")
	set("A3", "Trade Date", "Scrip Name", "ISIN", "Exchange", "Segment", "Trade Type", "Quantity", "Price", "Trade No", "Order No", "Trade Time")
	set("A4", "2024-06-03", "RELIANCE", "INE002A01018", "NSE", "EQ", "BUY", 10, 2252.35, "75217259", "1100000000000001", "10:15:22")
	set("A5", "2024-06-03", "RELIANCE", "INE002A01018", "NSE", "EQ", "SELL", "abc", 2252.35, "75217260", "1100000000000002", "10:15:22")
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	rows, issues, err := Parser{}.Parse(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, issues, 1)
	require.Equal(t, 4, rows[0].Line)
	require.Equal(t, 5, issues[0].Line)
	require.Equal(t, "2252.35", rows[0].Price.String())
}

func TestDetect(t *testing.T) {
	hdr, sample, err := parser.ReadCSVHead(strings.NewReader(tradeCSV), 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(hdr, sample), 0.9)

	zerodha := []string{"symbol", "isin", "trade_date", "exchange", "segment", "series", "trade_type", "auction", "quantity", "price", "trade_id", "order_id", "order_execution_time"}
	groww := []string{"stock name", "symbol", "isin", "type", "quantity", "value", "exchange", "exchange order id", "execution date and time", "order status"}
	angel := []string{"scrip/contract", "buy/sell", "buy price", "sell price", "quantity", "brokerage", "order type", "segment", "date"}
	for _, foreign := range [][]string{zerodha, groww, angel, {"a", "b", "c"}} {
		require.Less(t, Parser{}.Detect(foreign, nil), 0.3, foreign)
	}

	p, det, ok := parser.Best([]byte(tradeCSV))
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
	require.Equal(t, 6, issues[0].Line)
}

func TestNotUpstox(t *testing.T) {
	_, _, err := Parser{}.Parse(strings.NewReader("a,b,c\n1,2,3\n"))
	require.Error(t, err)
}
