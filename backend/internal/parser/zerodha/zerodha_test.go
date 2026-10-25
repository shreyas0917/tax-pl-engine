package zerodha

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
)

// Console exports start with a UTF-8 byte order mark; build it from bytes so the source stays clean.
var bom = string([]byte{0xEF, 0xBB, 0xBF})

var equityCSV = bom + "symbol,isin,trade_date,exchange,segment,series,trade_type,auction,quantity,price,trade_id,order_id,order_execution_time\n" +
	"INFY,INE009A01021,2024-06-03,NSE,EQ,EQ,buy,false,10,1400.5,100,1000,2024-06-03T10:15:22\n" +
	"INFY,INE009A01021,2025-02-10,NSE,EQ,EQ,sell,false,10,\"1,500.00\",101,1010,2025-02-10T14:01:02\n" +
	"\n" +
	"BADROW,INE000000000,2025-02-10,NSE,EQ,EQ,hold,false,10,1500,102,1020,2025-02-10T14:01:02\n" +
	"NOQTY,INE000000001,2025-02-11,NSE,EQ,EQ,buy,false,0,1500,103,1030,2025-02-11T14:01:02\n" +
	"NODATE,INE000000002,yesterday,NSE,EQ,EQ,buy,false,1,1500,104,1040,\n"

const fnoCSV = "symbol,isin,trade_date,exchange,segment,series,trade_type,auction,quantity,price,trade_id,order_id,order_execution_time,expiry_date\n" +
	"NIFTY24JUN23000CE,,2024-06-03,NFO,FO,CE,buy,false,50,100,200,2000,2024-06-03T09:30:00,2024-06-27\n" +
	"BANKNIFTY24JUNFUT,,2024-06-03,NFO,FO,FUT,sell,false,15,50000,201,2010,2024-06-03T09:31:00,2024-06-26\n" +
	"WEIRD,,2024-06-03,NFO,FO,,buy,false,15,10,202,2020,2024-06-03T09:31:00,2024-06-26\n"

func TestParseEquity(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(equityCSV))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, issues, 3, "every bad row is reported, none dropped silently")
	require.Equal(t, 5, issues[0].Line)
	require.Contains(t, issues[0].Reason, "trade type")
	require.Contains(t, issues[1].Reason, "quantity")
	require.Contains(t, issues[2].Reason, "trade_date")

	r := rows[0]
	require.Equal(t, "INFY", r.Symbol)
	require.Equal(t, "INE009A01021", r.ISIN)
	require.Equal(t, domain.Buy, r.Side)
	require.Equal(t, domain.SegEquity, r.Segment)
	require.Equal(t, "10", r.Quantity.String())
	require.Equal(t, "1400.5", r.Price.String())
	require.Equal(t, "2024-06-03", r.TradeDate.String())
	require.Equal(t, "10:15:22", r.TradeTime)
	require.Equal(t, "100", r.TradeID)
	require.Equal(t, 2, r.Line)
	require.Equal(t, "1500", rows[1].Price.String(), "thousands separators inside quotes are handled")
}

func TestParseFNO(t *testing.T) {
	rows, issues, err := Parser{}.Parse(strings.NewReader(fnoCSV))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Len(t, issues, 1)
	require.Contains(t, issues[0].Reason, "F&O symbol")
	opt := rows[0]
	require.Equal(t, domain.SegOption, opt.Segment)
	require.Equal(t, "NIFTY", opt.Symbol)
	require.Equal(t, "23000", opt.Strike.String())
	require.Equal(t, "CE", opt.OptionType)
	require.Equal(t, "2024-06-27", opt.Expiry.String())
	fut := rows[1]
	require.Equal(t, domain.SegFuture, fut.Segment)
	require.Equal(t, "BANKNIFTY", fut.Symbol)
	require.Equal(t, domain.Sell, fut.Side)
}

func TestDetect(t *testing.T) {
	header, sample, err := parser.ReadCSVHead(strings.NewReader(equityCSV), 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, Parser{}.Detect(header, sample), 0.9)
	require.Less(t, Parser{}.Detect([]string{"date", "scrip", "qty"}, nil), 0.3)
	p, det, ok := parser.Best([]byte(equityCSV))
	require.True(t, ok)
	require.Equal(t, "zerodha", det.Broker)
	require.Equal(t, "zerodha", p.Broker())
}

func TestNotZerodha(t *testing.T) {
	_, _, err := Parser{}.Parse(strings.NewReader("a,b,c\n1,2,3\n"))
	require.Error(t, err)
}
