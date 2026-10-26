package angelone_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/parser"
	_ "github.com/shreyas0917/taxpl/internal/parser/all" // every broker registered
)

// With every parser registered, each broker's sample must still be auto-detected as its own.
func TestRegistryPicksTheRightBroker(t *testing.T) {
	samples := map[string]string{
		"angelone": "testdata/sample.csv",
		"groww":    "../groww/testdata/sample.csv",
		"upstox":   "../upstox/testdata/sample.csv",
	}
	for broker, path := range samples {
		data, err := os.ReadFile(path)
		require.NoError(t, err, path)
		p, det, ok := parser.Best(data)
		require.True(t, ok, broker)
		require.Equal(t, broker, p.Broker())
		require.GreaterOrEqual(t, det.Confidence, 0.9, broker)
		for _, d := range parser.Detect(data)[1:] {
			if d.Broker == "zerodha" {
				continue // zerodha scores generic columns (symbol, isin, quantity, exchange) at up to 0.4
			}
			require.Less(t, d.Confidence, 0.3, "%s sample scored by %s", broker, d.Broker)
		}
	}
	zerodha := "symbol,isin,trade_date,exchange,segment,series,trade_type,auction,quantity,price,trade_id,order_id,order_execution_time\n" +
		"INFY,INE009A01021,2024-06-03,NSE,EQ,EQ,buy,false,10,1400.5,100,1000,2024-06-03T10:15:22\n"
	p, _, ok := parser.Best([]byte(zerodha))
	require.True(t, ok)
	require.Equal(t, "zerodha", p.Broker())
	for _, d := range parser.Detect([]byte(zerodha))[1:] {
		require.Less(t, d.Confidence, 0.3, d.Broker)
	}
}
