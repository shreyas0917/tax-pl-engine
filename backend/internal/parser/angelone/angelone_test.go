package angelone

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

const header = "Scrip/Contract,Buy/Sell,Buy Price,Sell Price,Quantity,Brokerage,Order Type,Segment,Date,GST,STT,SEBI Tax,Exchange Turnover Charges,Stamp Duty,Other Charges,IPFT charges\n"

var tradesCSV = bom + header +
	"INFY,Buy,1400.5,0,10,20,Delivery,Capital,03-06-2024,3.6,14.01,0.01,0.46,2.1,0.5,0.25\n" +
	"INFY,Sell,0,\"1,500.00\",10,20,Intraday,Capital,2025-02-10 14:01:02,3.6,15,0.02,0.49,0,0,0\n" +
	"\n" +
	"NIFTY 27JUN24 23000 CE,Buy,100,0,50,20,Normal,F&O,03-06-2024,3.6,0,0.01,2.5,0.15,0,0\n" +
	"BANKNIFTY 26JUN2024 FUT,Sell,0,50000,15,20,Normal,F&O,03-06-2024,3.6,93.75,0.08,14.6,0,0,0\n" +
	"WEIRD,Buy,10,0,15,20,Normal,F&O,03-06-2024,0,0,0,0,0,0,0\n" +
	"TCS,Hold,100,0,1,20,Delivery,Capital,04-06-2024,0,0,0,0,0,0,0\n" +
	"TCS,Buy,100,0,1,abc,Delivery,Capital,04-06-2024,0,0,0,0,0,0,0\n" +
	"TCS,Buy,100,0,1,20,Delivery,Capital,someday,0,0,0,0,0,0,0\n"

func TestParse(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(tradesCSV))
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Len(t, issues, 4, "every bad row is reported, none dropped silently")
	require.Equal(t, 7, issues[0].Line)
	require.Contains(t, issues[0].Reason, `cannot decode F&O contract "WEIRD"`)
	require.Contains(t, issues[1].Reason, "trade type")
	require.Contains(t, issues[2].Reason, `brokerage "abc"`)
	require.Contains(t, issues[3].Reason, "date")

	buy := rows[0]
	require.Equal(t, 2, buy.Line)
	require.Equal(t, "INFY", buy.Symbol)
	require.Equal(t, "", buy.ISIN, "the export has no ISIN; the normaliser resolves it")
	require.Equal(t, domain.SegEquity, buy.Segment)
	require.Equal(t, domain.KindRegular, buy.Kind)
	require.Equal(t, domain.Buy, buy.Side)
	require.Equal(t, "10", buy.Quantity.String())
	require.Equal(t, "1400.5", buy.Price.String(), "buys take Buy Price")
	require.Equal(t, "2024-06-03", buy.TradeDate.String())
	require.Equal(t, "Delivery", buy.Raw["order type"])
	require.NotNil(t, buy.Charges)
	require.Equal(t, "imported", buy.Charges.Source)
	require.Equal(t, "20", buy.Charges.Brokerage.String())
	require.Equal(t, "3.6", buy.Charges.GST.String())
	require.Equal(t, "14.01", buy.Charges.STT.String())
	require.Equal(t, "0.01", buy.Charges.SEBIFee.String())
	require.Equal(t, "1.21", buy.Charges.ExchangeFee.String(), "turnover + other + IPFT")
	require.Equal(t, "2.1", buy.Charges.StampDuty.String())

	sell := rows[1]
	require.Equal(t, domain.Sell, sell.Side)
	require.Equal(t, "1500", sell.Price.String(), "sells take Sell Price; thousands separators are handled")
	require.Equal(t, "2025-02-10", sell.TradeDate.String())
	require.Equal(t, "14:01:02", sell.TradeTime)

	opt := rows[2]
	require.Equal(t, domain.SegOption, opt.Segment)
	require.Equal(t, "NIFTY", opt.Symbol)
	require.Equal(t, "2024-06-27", opt.Expiry.String())
	require.Equal(t, "23000", opt.Strike.String())
	require.Equal(t, "CE", opt.OptionType)
	require.Equal(t, "100", opt.Price.String())

	fut := rows[3]
	require.Equal(t, domain.SegFuture, fut.Segment)
	require.Equal(t, "BANKNIFTY", fut.Symbol)
	require.Equal(t, "2024-06-26", fut.Expiry.String())
	require.True(t, fut.Strike.IsZero())
	require.Equal(t, "50000", fut.Price.String())
	require.Equal(t, "93.75", fut.Charges.STT.String())
}

func TestParseXLSXPrefersTradesAndChargesSheet(t *testing.T) {
	f := excelize.NewFile()
	first := f.GetSheetName(0)
	require.NoError(t, f.SetSheetRow(first, "A1", &[]interface{}{"Summary", "nothing useful"}))
	_, err := f.NewSheet("TradesAndCharges")
	require.NoError(t, err)
	set := func(cell string, vals ...interface{}) {
		require.NoError(t, f.SetSheetRow("TradesAndCharges", cell, &vals))
	}
	set("A1", "ClientCode", "A123456")
	set("A3", "Scrip/Contract", "Buy/Sell", "Buy Price", "Sell Price", "Quantity", "Brokerage", "Order Type", "Segment", "Date", "GST", "STT")
	set("A4", "INFY", "Buy", 1400.5, 0, 10, 20, "Delivery", "Capital", "03-06-2024", 3.6, 14.01)
	set("A5", "NIFTY27JUN2423000PE", "Sell", 0, 55.5, 50, 20, "Normal", "F&O", "03-06-2024", 3.6, 0)
	set("A6", "INFY", "Buy", 1400.5, 0, 0, 20, "Delivery", "Capital", "03-06-2024", 3.6, 14.01)
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)

	rows, issues, err := Parser{}.Parse(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, issues, 1)
	require.Equal(t, 6, issues[0].Line)
	require.Contains(t, issues[0].Reason, "quantity")
	require.Equal(t, 4, rows[0].Line)
	require.Equal(t, "1400.5", rows[0].Price.String())
	require.Equal(t, "14.01", rows[0].Charges.STT.String())
	require.True(t, rows[0].Charges.StampDuty.IsZero(), "absent charge columns stay zero")
	require.Equal(t, domain.SegOption, rows[1].Segment)
	require.Equal(t, "PE", rows[1].OptionType)
	require.Equal(t, "23000", rows[1].Strike.String())
	require.Equal(t, "2024-06-27", rows[1].Expiry.String())
	require.Equal(t, "55.5", rows[1].Price.String())
}

func TestExplicitContractColumnsWin(t *testing.T) {
	csv := "Scrip/Contract,Buy/Sell,Price,Quantity,Segment,Date,Expiry Date,Strike Price,Option Type\n" +
		"NIFTY,Buy,100,50,F&O,03-06-2024,27-06-2024,23000,CE\n" +
		"BANKNIFTY,Sell,50000,15,F&O,03-06-2024,26-06-2024,0,\n" +
		"NIFTY,Sell,50000,15,F&O,03-06-2024,,0,\n"
	rows, issues, err := Parser{}.Parse(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Reason, "cannot decode F&O contract")
	require.Equal(t, domain.SegOption, rows[0].Segment)
	require.Equal(t, "2024-06-27", rows[0].Expiry.String())
	require.Equal(t, domain.SegFuture, rows[1].Segment)
	require.Equal(t, "2024-06-26", rows[1].Expiry.String())
	require.Nil(t, rows[0].Charges, "no charge columns, no Charges")
}

func TestDetect(t *testing.T) {
	hdr, sample, err := parser.ReadCSVHead(strings.NewReader(tradesCSV), 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(hdr, sample), 0.9)

	zerodha := []string{"symbol", "isin", "trade_date", "exchange", "segment", "series", "trade_type", "auction", "quantity", "price", "trade_id", "order_id", "order_execution_time"}
	groww := []string{"stock name", "symbol", "isin", "type", "quantity", "value", "exchange", "exchange order id", "execution date and time", "order status"}
	upstox := []string{"trade date", "scrip name", "isin", "exchange", "segment", "trade type", "quantity", "price", "trade no", "order no", "trade time", "expiry", "strike", "option type"}
	for _, foreign := range [][]string{zerodha, groww, upstox, {"a", "b", "c"}} {
		require.Less(t, Parser{}.Detect(foreign, nil), 0.3, foreign)
	}

	// The header hides behind a preamble in the real export; the sample rows still reveal it.
	preamble := "ClientCode,A123456\n\n" + tradesCSV[len(bom):]
	hdr, sample, err = parser.ReadCSVHead(strings.NewReader(preamble), 5)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(hdr, sample), 0.9)

	p, det, ok := parser.Best([]byte(tradesCSV))
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

func TestNotAngelOne(t *testing.T) {
	_, _, err := Parser{}.Parse(strings.NewReader("a,b,c\n1,2,3\n"))
	require.Error(t, err)
}
