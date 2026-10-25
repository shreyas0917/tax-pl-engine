package matcher_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/matcher"
)

// genTrades builds a random but valid equity trade history across a few accounts and ISINs.
func genTrades(t *rapid.T) []domain.Trade {
	accounts := []string{"A", "B"}
	isins := []string{"INE1", "INE2", "INE3"}
	n := rapid.IntRange(0, 60).Draw(t, "n")
	start := domain.MustDate("2023-04-01")
	var out []domain.Trade
	for i := 0; i < n; i++ {
		tr := domain.Trade{
			ID:        fmt.Sprintf("t%03d", i),
			AccountID: rapid.SampledFrom(accounts).Draw(t, "acct"),
			ISIN:      rapid.SampledFrom(isins).Draw(t, "isin"),
			Segment:   domain.SegEquity,
			Kind:      domain.KindRegular,
			Quantity:  decimal.NewFromInt(int64(rapid.IntRange(1, 50).Draw(t, "qty"))),
			Price:     decimal.NewFromInt(int64(rapid.IntRange(1, 1000).Draw(t, "price"))).Add(decimal.NewFromInt(int64(rapid.IntRange(0, 99).Draw(t, "paise"))).Div(decimal.NewFromInt(100))),
			TradeDate: start.AddDays(rapid.IntRange(0, 900).Draw(t, "day")),
			TradeTime: fmt.Sprintf("%02d:%02d:00", rapid.IntRange(9, 15).Draw(t, "h"), rapid.IntRange(0, 59).Draw(t, "m")),
			TradeRef:  fmt.Sprintf("r%03d", i),
		}
		if rapid.Bool().Draw(t, "sell") {
			tr.Side = domain.Sell
		} else {
			tr.Side = domain.Buy
		}
		tr.Symbol = tr.ISIN
		out = append(out, tr)
	}
	return out
}

func sum(ds []decimal.Decimal) decimal.Decimal {
	s := decimal.Zero
	for _, d := range ds {
		s = s.Add(d)
	}
	return s
}

func TestPropertyFIFOInvariants(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		trades := genTrades(rt)
		netting := rapid.Bool().Draw(rt, "netting")
		res, err := matcher.Match(matcher.Input{Trades: trades, Config: matcher.Config{IntradayNetting: netting}})
		require.NoError(t, err)

		// Per (account, ISIN): bought = matched + intraday-buy-side + open; sold = matched + intraday + unmatched.
		type key struct{ a, i string }
		bought, sold := map[key]decimal.Decimal{}, map[key]decimal.Decimal{}
		for _, tr := range trades {
			k := key{tr.AccountID, tr.ISIN}
			if tr.Side == domain.Buy {
				bought[k] = bought[k].Add(tr.Quantity)
			} else {
				sold[k] = sold[k].Add(tr.Quantity)
			}
		}
		matched, intra, open := map[key]decimal.Decimal{}, map[key]decimal.Decimal{}, map[key]decimal.Decimal{}
		for _, m := range res.Matches {
			k := key{m.AccountID, m.ISIN}
			matched[k] = matched[k].Add(m.Quantity)
			require.True(t, m.Quantity.IsPositive(), "match quantity must be positive")
			require.False(t, m.CostBasis.IsNegative(), "cost basis never negative")
			require.False(t, m.SaleDate.Before(m.AcquisitionDate), "sale before acquisition")
		}
		for _, m := range res.Intraday {
			k := key{m.AccountID, m.ISIN}
			intra[k] = intra[k].Add(m.Quantity)
		}
		for _, l := range res.Lots {
			require.False(t, l.QuantityOpen.IsNegative(), "lot quantity never negative")
			require.False(t, l.CostOpen.IsNegative(), "lot cost never negative")
			require.True(t, l.QuantityOpen.LessThanOrEqual(l.QuantityOriginal), "open <= original")
			k := key{l.AccountID, l.ISIN}
			open[k] = open[k].Add(l.QuantityOpen)
		}
		unmatched := map[key]decimal.Decimal{}
		for _, r := range res.Review {
			if r.Kind == domain.ReviewMissingBuy {
				q, _ := decimal.NewFromString(r.Payload["quantity"])
				unmatched[key{r.AccountID, r.ISIN}] = unmatched[key{r.AccountID, r.ISIN}].Add(q)
			}
		}
		for k := range bought {
			require.True(t, matched[k].Add(intra[k]).Add(open[k]).Equal(bought[k]), "%v: bought %s != matched %s + intraday %s + open %s", k, bought[k], matched[k], intra[k], open[k])
		}
		for k := range sold {
			require.True(t, matched[k].Add(intra[k]).Add(unmatched[k]).Equal(sold[k]), "%v: sold %s != matched %s + intraday %s + unmatched %s", k, sold[k], matched[k], intra[k], unmatched[k])
			require.True(t, matched[k].LessThanOrEqual(bought[k]), "never sells more than was bought")
		}

		// Cost conservation: total cost of lots = matched cost + open cost (pro-rata rounding sums exactly).
		lotCost := map[string]decimal.Decimal{}
		for _, l := range res.Lots {
			lotCost[l.ID] = l.CostOriginal
		}
		consumed := map[string]decimal.Decimal{}
		for _, m := range res.Matches {
			consumed[m.LotID] = consumed[m.LotID].Add(m.CostBasis)
		}
		for _, l := range res.Lots {
			require.True(t, consumed[l.ID].Add(l.CostOpen).Equal(l.CostOriginal), "lot %s: consumed %s + open %s != original %s", l.ID, consumed[l.ID], l.CostOpen, l.CostOriginal)
		}

		// Sum of (sale - cost) over matches equals the sum of match-level gains (identity the classifier relies on).
		var gains, sv, cb []decimal.Decimal
		for _, m := range res.Matches {
			gains = append(gains, m.SaleValue.Sub(m.CostBasis))
			sv = append(sv, m.SaleValue)
			cb = append(cb, m.CostBasis)
		}
		require.True(t, sum(gains).Equal(sum(sv).Sub(sum(cb))))

		// FIFO: within an (account, ISIN) a later sale never consumes an older lot than an earlier sale left open.
		// Equivalent check: matches for the same sell trade are in non-decreasing acquisition date order.
		lastSell, lastAcq := "", domain.Date{}
		for _, m := range res.Matches {
			if m.SellTradeID == lastSell {
				require.False(t, m.AcquisitionDate.Before(lastAcq), "FIFO order violated within sale %s", m.SellTradeID)
			}
			lastSell, lastAcq = m.SellTradeID, m.AcquisitionDate
		}
	})
}

func TestPropertyInputOrderIndependence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		trades := genTrades(rt)
		res1, err := matcher.Match(matcher.Input{Trades: trades, Config: matcher.Config{IntradayNetting: true}})
		require.NoError(t, err)
		shuffled := append([]domain.Trade(nil), trades...)
		seed := rapid.Int64().Draw(rt, "seed")
		rand.New(rand.NewSource(seed)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		res2, err := matcher.Match(matcher.Input{Trades: shuffled, Config: matcher.Config{IntradayNetting: true}})
		require.NoError(t, err)
		require.Equal(t, res1.Matches, res2.Matches)
		require.Equal(t, res1.Intraday, res2.Intraday)
		require.Equal(t, res1.Lots, res2.Lots)
		require.Equal(t, res1.Review, res2.Review)
	})
}

func TestPropertyAccountsAreIndependent(t *testing.T) {
	// Matching account A alone must equal A's slice of matching A and B together.
	rapid.Check(t, func(rt *rapid.T) {
		trades := genTrades(rt)
		var onlyA []domain.Trade
		for _, tr := range trades {
			if tr.AccountID == "A" {
				onlyA = append(onlyA, tr)
			}
		}
		all, err := matcher.Match(matcher.Input{Trades: trades, Config: matcher.Config{IntradayNetting: true}})
		require.NoError(t, err)
		a, err := matcher.Match(matcher.Input{Trades: onlyA, Config: matcher.Config{IntradayNetting: true}})
		require.NoError(t, err)
		var fromAll []domain.LotMatch
		for _, m := range all.Matches {
			if m.AccountID == "A" {
				fromAll = append(fromAll, m)
			}
		}
		require.Equal(t, a.Matches, fromAll)
	})
}

func TestPropertyNettingNeverExceedsMinBuySell(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		trades := genTrades(rt)
		res, err := matcher.Match(matcher.Input{Trades: trades, Config: matcher.Config{IntradayNetting: true}})
		require.NoError(t, err)
		type key struct {
			a, i string
			d    domain.Date
		}
		buy, sell, intra := map[key]decimal.Decimal{}, map[key]decimal.Decimal{}, map[key]decimal.Decimal{}
		for _, tr := range trades {
			k := key{tr.AccountID, tr.ISIN, tr.TradeDate}
			if tr.Side == domain.Buy {
				buy[k] = buy[k].Add(tr.Quantity)
			} else {
				sell[k] = sell[k].Add(tr.Quantity)
			}
		}
		for _, m := range res.Intraday {
			k := key{m.AccountID, m.ISIN, m.Date}
			intra[k] = intra[k].Add(m.Quantity)
		}
		for k, q := range intra {
			require.True(t, q.Equal(domain.Min(buy[k], sell[k])), "%v intraday %s != min(buy %s, sell %s)", k, q, buy[k], sell[k])
		}
	})
}
