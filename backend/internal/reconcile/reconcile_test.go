package reconcile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/domain"
)

const zerodhaHoldings = "Symbol,ISIN,Sector,Quantity Available,Quantity Discrepant,Quantity Long Term,Average Price,Previous Closing Price\n" +
	"INFY,INE009A01021,IT,100,0,100,1400.50,1500\n" +
	"TCS,INE467B01029,IT,10,0,0,3800,3900\n" +
	"SBIN,INE062A01020,Banks,50,0,50,600,800\n" +
	"BAD,INE000000000,X,abc,0,0,1,1\n"

func lot(isin, sym, qty, cost string) domain.Lot {
	return domain.Lot{AccountID: "A", ISIN: isin, Symbol: sym, QuantityOpen: domain.D(qty), CostOpen: domain.D(cost), QuantityOriginal: domain.D(qty), CostOriginal: domain.D(cost)}
}

func TestParseHoldings(t *testing.T) {
	hs, issues, err := ParseHoldings(strings.NewReader(zerodhaHoldings))
	require.NoError(t, err)
	require.Len(t, hs, 3)
	require.Len(t, issues, 1)
	require.Equal(t, 5, issues[0].Line)
	require.Equal(t, "INE009A01021", hs[0].ISIN)
	require.Equal(t, "100", hs[0].Quantity.String())
	require.Equal(t, "1400.5", hs[0].AvgPrice.String())
	_, _, err = ParseHoldings(strings.NewReader("a,b\n1,2\n"))
	require.Error(t, err)
}

func TestCompare(t *testing.T) {
	hs, _, _ := ParseHoldings(strings.NewReader(zerodhaHoldings))
	lots := []domain.Lot{
		lot("INE009A01021", "INFY", "100", "140050"),  // matches quantity and cost
		lot("INE467B01029", "TCS", "4", "15200"),      // broker has 10: 6 missing
		lot("INE002A01018", "RELIANCE", "7", "17500"), // engine-only
		{AccountID: "B", ISIN: "INE062A01020", Symbol: "SBIN", QuantityOpen: domain.D("50"), CostOpen: domain.D("30000")}, // other account, ignored
	}
	res := Compare("A", hs, lots, nil)
	require.Equal(t, 1, res.Matched)
	kinds := map[string]Difference{}
	for _, d := range res.Differences {
		kinds[d.Kind+":"+d.Symbol] = d
	}
	require.Contains(t, kinds, "MISSING_HISTORY:TCS")
	require.Equal(t, "6", kinds["MISSING_HISTORY:TCS"].Delta.String())
	require.Contains(t, kinds, "MISSING_HISTORY:SBIN", "SBIN is held in account B, not A")
	require.Contains(t, kinds, "EXTRA_IN_ENGINE:RELIANCE")
	require.Equal(t, "-7", kinds["EXTRA_IN_ENGINE:RELIANCE"].Delta.String())
	require.Len(t, res.Differences, 3)

	// Cost mismatch beyond 1% is flagged even when the quantity matches.
	res2 := Compare("A", hs[:1], []domain.Lot{lot("INE009A01021", "INFY", "100", "100000")}, nil)
	require.Equal(t, 1, res2.Matched)
	require.Len(t, res2.Differences, 1)
	require.Equal(t, "COST_MISMATCH", res2.Differences[0].Kind)
}
