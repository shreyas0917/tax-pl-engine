package tax

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/shreyas0917/taxpl/internal/domain"
)

func TestRulesLoadAndValidate(t *testing.T) {
	require.Equal(t, []string{"2023-24", "2024-25", "2025-26", "2026-27"}, Years())
	r, err := Load("2024-25")
	require.NoError(t, err)
	require.Equal(t, "fy2024-25@1", r.VersionString())
	require.Len(t, r.Equity.Periods, 2)
	require.Equal(t, "Income-tax Act, 1961", r.Law)
	require.False(t, r.IsNewAct())
	p, ok := r.PeriodFor(domain.MustDate("2024-07-23"))
	require.True(t, ok)
	require.Equal(t, "0.125", p.LTCGRate.String())
	p, ok = r.PeriodFor(domain.MustDate("2024-07-22"))
	require.True(t, ok)
	require.Equal(t, "0.1", p.LTCGRate.String())

	ty, err := Load("2026-27")
	require.NoError(t, err)
	require.True(t, ty.IsNewAct())
	require.Equal(t, "ty2026-27@1", ty.VersionString())
	require.Equal(t, "s.198", ty.References["ltcg"])
	require.Equal(t, BuybackCapitalGains, ty.Buyback.RegimeOn(domain.MustDate("2026-05-10")))

	fy25, _ := Load("2024-25")
	require.Equal(t, BuybackExempt115QA, fy25.Buyback.RegimeOn(domain.MustDate("2024-09-30")))
	require.Equal(t, BuybackDeemedDividend, fy25.Buyback.RegimeOn(domain.MustDate("2024-10-01")))

	_, err = Load("1999-00")
	require.Error(t, err)
}

func TestIsLongTermBoundary(t *testing.T) {
	r, _ := Load("2025-26")
	acq := domain.MustDate("2024-08-10")
	require.False(t, IsLongTerm(acq, domain.MustDate("2025-08-09"), r), "day before anniversary is short-term")
	require.True(t, IsLongTerm(acq, domain.MustDate("2025-08-10"), r), "anniversary is long-term (Delhi HC, Bharti Gupta Ramola)")
	require.True(t, IsLongTerm(acq, domain.MustDate("2025-08-11"), r))
	require.False(t, IsLongTerm(acq, acq, r))

	strict := *r
	strict.Equity.LongTermBoundary = "after_anniversary"
	require.False(t, IsLongTerm(acq, domain.MustDate("2025-08-10"), &strict), "configurable stricter reading")
	require.True(t, IsLongTerm(acq, domain.MustDate("2025-08-11"), &strict))

	// Leap-day purchase: 12 calendar months end on 28 Feb of the next year.
	leap := domain.MustDate("2024-02-29")
	require.False(t, IsLongTerm(leap, domain.MustDate("2025-02-27"), r))
	require.True(t, IsLongTerm(leap, domain.MustDate("2025-02-28"), r))
}

func TestPropertyLongTermMonotonic(t *testing.T) {
	r, _ := Load("2025-26")
	rapid.Check(t, func(rt *rapid.T) {
		acq := domain.MustDate("2015-01-01").AddDays(rapid.IntRange(0, 4000).Draw(rt, "acq"))
		d1 := acq.AddDays(rapid.IntRange(0, 800).Draw(rt, "d1"))
		d2 := d1.AddDays(rapid.IntRange(0, 800).Draw(rt, "d2"))
		// Monotonic: once long-term, always long-term for later sales.
		if IsLongTerm(acq, d1, r) {
			require.True(t, IsLongTerm(acq, d2, r))
		}
		// Day-count sanity: < 365 days is never long-term; >= 366 days is always long-term.
		days := acq.DaysUntil(d1)
		if days < 365 {
			require.False(t, IsLongTerm(acq, d1, r), "%s -> %s (%d days)", acq, d1, days)
		}
		if days >= 366 {
			require.True(t, IsLongTerm(acq, d1, r), "%s -> %s (%d days)", acq, d1, days)
		}
	})
}

func TestInrFormatting(t *testing.T) {
	require.Equal(t, "Rs 1,00,00,000", inr(domain.D("10000000")))
	require.Equal(t, "Rs 10,00,00,000", inr(domain.D("100000000")))
	require.Equal(t, "Rs 999", inr(domain.D("999")))
	require.Equal(t, "Rs 1,000", inr(domain.D("1000")))
	require.Equal(t, "Rs 1,25,000", inr(domain.D("125000")))
}
