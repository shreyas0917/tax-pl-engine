package corpaction

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/domain"
)

func lot(qty, cost string, fmv string) *domain.Lot {
	l := &domain.Lot{ID: "L1", AccountID: "A", ISIN: "INE1", Symbol: "X", Origin: domain.OriginTrade,
		QuantityOriginal: domain.D(qty), QuantityOpen: domain.D(qty), CostOriginal: domain.D(cost), CostOpen: domain.D(cost),
		AcquisitionDate: domain.MustDate("2017-06-01")}
	if fmv != "" {
		f := domain.D(fmv)
		l.GrandfatheredFMV = &f
	}
	return l
}

func TestSplitKeepsTotalCostAndTotalFMV(t *testing.T) {
	l := lot("100", "30000", "605")
	res, err := Apply(domain.CorporateAction{ISIN: "INE1", Kind: domain.CASplit, ExDate: domain.MustDate("2019-09-20"), RatioFrom: domain.D("1"), RatioTo: domain.D("2")}, []*domain.Lot{l})
	require.NoError(t, err)
	require.Empty(t, res.NewLots)
	require.Equal(t, "200", l.QuantityOpen.String())
	require.Equal(t, "30000", l.CostOpen.String())
	require.Equal(t, "302.5", l.GrandfatheredFMV.String())
	require.Equal(t, "2017-06-01", l.AcquisitionDate.String())
	require.Len(t, l.Adjustments, 1)
	require.Equal(t, "SPLIT", l.Adjustments[0].Kind)
}

func TestSplitSkipsClosedLots(t *testing.T) {
	l := lot("100", "30000", "")
	l.QuantityOpen, l.CostOpen = domain.Zero, domain.Zero
	_, err := Apply(domain.CorporateAction{ISIN: "INE1", Kind: domain.CASplit, ExDate: domain.MustDate("2019-09-20"), RatioFrom: domain.D("1"), RatioTo: domain.D("5")}, []*domain.Lot{l})
	require.NoError(t, err)
	require.True(t, l.QuantityOpen.IsZero())
	require.Empty(t, l.Adjustments)
}

func TestBonusCreatesNilCostLot(t *testing.T) {
	l := lot("10", "10000", "")
	res, err := Apply(domain.CorporateAction{ID: "b", ISIN: "INE1", Kind: domain.CABonus, ExDate: domain.MustDate("2025-01-15"), RatioFrom: domain.D("2"), RatioTo: domain.D("1")}, []*domain.Lot{l})
	require.NoError(t, err)
	require.Len(t, res.NewLots, 1)
	nl := res.NewLots[0]
	require.Equal(t, "5", nl.QuantityOpen.String())
	require.True(t, nl.CostOpen.IsZero())
	require.Equal(t, "2025-01-15", nl.AcquisitionDate.String())
	require.Equal(t, domain.OriginBonus, nl.Origin)
	require.Equal(t, "10", l.QuantityOpen.String(), "original lot unchanged")
	require.Equal(t, "10000", l.CostOpen.String())
}

func TestDemergerApportionsCost(t *testing.T) {
	l := lot("100", "100000", "")
	res, err := Apply(domain.CorporateAction{ID: "d", ISIN: "INE1", Kind: domain.CADemerger, ExDate: domain.MustDate("2024-10-01"), RatioFrom: domain.D("1"), RatioTo: domain.D("1"),
		CostSplitPct: domain.D("0.30"), NewISIN: "INE2", NewSymbol: "NEWCO"}, []*domain.Lot{l})
	require.NoError(t, err)
	require.Equal(t, "70000", l.CostOpen.String())
	require.Equal(t, "100", l.QuantityOpen.String())
	require.Len(t, res.NewLots, 1)
	nl := res.NewLots[0]
	require.Equal(t, "INE2", nl.ISIN)
	require.Equal(t, "30000", nl.CostOpen.String())
	require.Equal(t, "100", nl.QuantityOpen.String())
	require.Equal(t, "2017-06-01", nl.AcquisitionDate.String(), "acquisition date carries over")
	require.True(t, l.CostOpen.Add(nl.CostOpen).Equal(domain.D("100000")), "cost is conserved")
}

func TestMergerMovesLot(t *testing.T) {
	l := lot("100", "50000", "400")
	res, err := Apply(domain.CorporateAction{ID: "m", ISIN: "INE1", Kind: domain.CAMerger, ExDate: domain.MustDate("2024-11-01"), RatioFrom: domain.D("10"), RatioTo: domain.D("7"), NewISIN: "INE2", NewSymbol: "MERGED"}, []*domain.Lot{l})
	require.NoError(t, err)
	require.Equal(t, "INE2", res.MovedToISIN)
	require.True(t, l.QuantityOpen.IsZero())
	nl := res.NewLots[0]
	require.Equal(t, "70", nl.QuantityOpen.String())
	require.Equal(t, "50000", nl.CostOpen.String())
	require.Equal(t, "2017-06-01", nl.AcquisitionDate.String())
	// Total FMV preserved: 100 x 400 = 40000 = 70 x 571.428...
	require.True(t, nl.GrandfatheredFMV.Mul(domain.D("70")).Round(2).Equal(domain.D("40000")))
}

func TestErrors(t *testing.T) {
	l := lot("1", "1", "")
	_, err := Apply(domain.CorporateAction{ISIN: "INE1", Kind: domain.CASplit, RatioFrom: domain.Zero, RatioTo: domain.D("2")}, []*domain.Lot{l})
	require.Error(t, err)
	_, err = Apply(domain.CorporateAction{ISIN: "INE1", Kind: domain.CADemerger, RatioFrom: domain.D("1"), RatioTo: domain.D("1")}, []*domain.Lot{l})
	require.Error(t, err, "demerger needs new_isin")
	_, err = Apply(domain.CorporateAction{ISIN: "INE1", Kind: "RIGHTS", RatioFrom: domain.D("1"), RatioTo: domain.D("1")}, []*domain.Lot{l})
	require.Error(t, err)
}
