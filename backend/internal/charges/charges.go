// Package charges estimates transaction costs when the tradebook carries none. Rates are
// dated data (an STT change on 1 April 2026 is one more row, not a code change) and every
// estimated value is labelled "estimated" so the UI and reports can say so. Pure.
package charges

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"github.com/shreyas0917/taxpl/internal/domain"
)

//go:embed schedule.yaml
var scheduleFS embed.FS

// Rate is one dated charge rate for an instrument class and side.
type Rate struct {
	From    domain.Date     `yaml:"from"`
	To      domain.Date     `yaml:"to"`      // zero = open-ended
	Segment string          `yaml:"segment"` // EQ_DELIVERY | EQ_INTRADAY | FUT | OPT
	Side    string          `yaml:"side"`    // BUY | SELL | BOTH
	Pct     decimal.Decimal `yaml:"pct"`     // fraction of trade value, e.g. 0.001 = 0.1%
	Source  string          `yaml:"source,omitempty"`
}

// BrokerPlan is one broker's brokerage model.
type BrokerPlan struct {
	Broker       string          `yaml:"broker"`
	DeliveryPct  decimal.Decimal `yaml:"delivery_pct"`  // fraction of trade value
	DeliveryFlat decimal.Decimal `yaml:"delivery_flat"` // per order, INR
	IntradayPct  decimal.Decimal `yaml:"intraday_pct"`
	IntradayFlat decimal.Decimal `yaml:"intraday_flat"`
	FNOFlat      decimal.Decimal `yaml:"fno_flat"`
	FNOPct       decimal.Decimal `yaml:"fno_pct"`
	MaxPerOrder  decimal.Decimal `yaml:"max_per_order"`
	Source       string          `yaml:"source,omitempty"`
}

// Schedule is the whole dated schedule.
type Schedule struct {
	Version  int             `yaml:"version"`
	STT      []Rate          `yaml:"stt"`
	Exchange []Rate          `yaml:"exchange"` // exchange transaction charges
	SEBI     []Rate          `yaml:"sebi"`
	Stamp    []Rate          `yaml:"stamp"`
	GSTPct   decimal.Decimal `yaml:"gst_pct"` // on brokerage + exchange + sebi
	Brokers  []BrokerPlan    `yaml:"brokers"`
	Sources  []string        `yaml:"sources"`
}

var (
	once    sync.Once
	loaded  *Schedule
	loadErr error
)

// Load returns the embedded schedule.
func Load() (*Schedule, error) {
	once.Do(func() {
		b, err := scheduleFS.ReadFile("schedule.yaml")
		if err != nil {
			loadErr = err
			return
		}
		var s Schedule
		if err := yaml.Unmarshal(b, &s); err != nil {
			loadErr = fmt.Errorf("charges schedule: %w", err)
			return
		}
		for _, list := range [][]Rate{s.STT, s.Exchange, s.SEBI, s.Stamp} {
			sort.SliceStable(list, func(i, j int) bool { return list[i].From.Before(list[j].From) })
		}
		loaded = &s
	})
	return loaded, loadErr
}

// Class says which charge class a trade falls in.
type Class string

// Classes.
const (
	ClassDelivery Class = "EQ_DELIVERY"
	ClassIntraday Class = "EQ_INTRADAY"
	ClassFuture   Class = "FUT"
	ClassOption   Class = "OPT"
)

// ClassOf maps a trade to its class. intraday tells whether the equity trade was squared off
// the same day (the matcher knows; the caller passes it).
func ClassOf(t domain.Trade, intraday bool) Class {
	switch t.Segment {
	case domain.SegFuture:
		return ClassFuture
	case domain.SegOption:
		return ClassOption
	}
	if intraday {
		return ClassIntraday
	}
	return ClassDelivery
}

func rateFor(list []Rate, class Class, side domain.Side, d domain.Date) (decimal.Decimal, bool) {
	for i := len(list) - 1; i >= 0; i-- {
		r := list[i]
		if r.Segment != string(class) || (r.Side != "BOTH" && r.Side != string(side)) {
			continue
		}
		if d.Before(r.From) || (!r.To.IsZero() && d.After(r.To)) {
			continue
		}
		return r.Pct, true
	}
	return decimal.Zero, false
}

// Estimate computes charges for a trade. Returned Charges have Source "estimated".
func (s *Schedule) Estimate(t domain.Trade, class Class) (domain.Charges, error) {
	value := t.Value()
	c := domain.Charges{Source: "estimated"}
	pick := func(list []Rate) decimal.Decimal {
		p, ok := rateFor(list, class, t.Side, t.TradeDate)
		if !ok {
			return decimal.Zero
		}
		return domain.RoundMoney(value.Mul(p))
	}
	c.STT = pick(s.STT)
	c.ExchangeFee = pick(s.Exchange)
	c.SEBIFee = pick(s.SEBI)
	c.StampDuty = pick(s.Stamp)
	plan := s.plan(t.Broker)
	switch class {
	case ClassDelivery:
		c.Brokerage = plan.DeliveryFlat.Add(domain.RoundMoney(value.Mul(plan.DeliveryPct)))
	case ClassIntraday:
		c.Brokerage = plan.IntradayFlat.Add(domain.RoundMoney(value.Mul(plan.IntradayPct)))
	default:
		c.Brokerage = plan.FNOFlat.Add(domain.RoundMoney(value.Mul(plan.FNOPct)))
	}
	if plan.MaxPerOrder.IsPositive() && c.Brokerage.GreaterThan(plan.MaxPerOrder) {
		c.Brokerage = plan.MaxPerOrder
	}
	c.GST = domain.RoundMoney(c.Brokerage.Add(c.ExchangeFee).Add(c.SEBIFee).Mul(s.GSTPct))
	return c, nil
}

func (s *Schedule) plan(broker string) BrokerPlan {
	broker = strings.ToLower(broker)
	var generic BrokerPlan
	for _, p := range s.Brokers {
		if p.Broker == broker {
			return p
		}
		if p.Broker == "default" {
			generic = p
		}
	}
	return generic
}

// STTRateOn is a convenience for docs and tests: the STT rate for a class/side on a date.
func (s *Schedule) STTRateOn(class Class, side domain.Side, d domain.Date) (decimal.Decimal, bool) {
	return rateFor(s.STT, class, side, d)
}
