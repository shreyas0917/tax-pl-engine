package charges

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shreyas0917/taxpl/internal/domain"
)

func TestSTTRatesAreDated(t *testing.T) {
	s, err := Load()
	require.NoError(t, err)
	cases := []struct {
		class Class
		side  domain.Side
		date  string
		want  string
	}{
		{ClassDelivery, domain.Buy, "2025-06-01", "0.001"},
		{ClassDelivery, domain.Sell, "2026-06-01", "0.001"}, // unchanged by Budget 2026
		{ClassIntraday, domain.Sell, "2025-06-01", "0.00025"},
		{ClassFuture, domain.Sell, "2024-09-30", "0.000125"},
		{ClassFuture, domain.Sell, "2024-10-01", "0.0002"},
		{ClassFuture, domain.Sell, "2026-03-31", "0.0002"},
		{ClassFuture, domain.Sell, "2026-04-01", "0.0005"}, // Finance Act 2026
		{ClassOption, domain.Sell, "2026-03-31", "0.001"},
		{ClassOption, domain.Sell, "2026-04-01", "0.0015"},
	}
	for _, c := range cases {
		got, ok := s.STTRateOn(c.class, c.side, domain.MustDate(c.date))
		require.True(t, ok, "%v %v %s", c.class, c.side, c.date)
		require.Equal(t, c.want, got.String(), "%v %v %s", c.class, c.side, c.date)
	}
	_, ok := s.STTRateOn(ClassIntraday, domain.Buy, domain.MustDate("2025-06-01"))
	require.False(t, ok, "no STT on the intraday buy side")
	_, ok = s.STTRateOn(ClassFuture, domain.Buy, domain.MustDate("2025-06-01"))
	require.False(t, ok, "no STT on futures buys")
}

func TestEstimateDeliverySell(t *testing.T) {
	s, err := Load()
	require.NoError(t, err)
	tr := domain.Trade{Broker: "zerodha", Segment: domain.SegEquity, Side: domain.Sell, Quantity: domain.D("100"), Price: domain.D("1000"), TradeDate: domain.MustDate("2025-06-02")}
	c, err := s.Estimate(tr, ClassDelivery)
	require.NoError(t, err)
	require.Equal(t, "estimated", c.Source)
	require.Equal(t, "100", c.STT.String(), "0.1% of 1,00,000")
	require.Equal(t, "0", c.Brokerage.String(), "Zerodha delivery is free")
	require.Equal(t, "0", c.StampDuty.String(), "stamp duty is on the buy side only")
	require.True(t, c.Total().GreaterThan(c.STT))
	require.True(t, c.NonSTT().Equal(c.Total().Sub(c.STT)))

	buy := tr
	buy.Side = domain.Buy
	cb, err := s.Estimate(buy, ClassDelivery)
	require.NoError(t, err)
	require.Equal(t, "15", cb.StampDuty.String(), "0.015% on the buy side")
}

func TestEstimateFNOAndUnknownBroker(t *testing.T) {
	s, err := Load()
	require.NoError(t, err)
	tr := domain.Trade{Broker: "someone-else", Segment: domain.SegOption, Side: domain.Sell, Quantity: domain.D("50"), Price: domain.D("100"), TradeDate: domain.MustDate("2026-05-05")}
	c, err := s.Estimate(tr, ClassOf(tr, false))
	require.NoError(t, err)
	require.Equal(t, "7.5", c.STT.String(), "0.15% of 5,000 from 1 Apr 2026")
	require.Equal(t, "20", c.Brokerage.String(), "default plan flat fee")
	require.Equal(t, "3.92", c.GST.StringFixed(2), "18% GST on brokerage 20 + exchange 1.75 + sebi 0.005")
}
