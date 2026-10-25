// Package tax classifies lot matches into tax categories using per-year rules files.
// It is pure: no I/O beyond the embedded rules.
package tax

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
	"gopkg.in/yaml.v3"

	"github.com/shreyas0917/taxpl/internal/domain"
)

//go:embed rules/*.yaml
var rulesFS embed.FS

// RatePeriod is a date range with its STCG/LTCG rates. Rates changed on 23 Jul 2024.
type RatePeriod struct {
	From     domain.Date     `yaml:"from"`
	To       domain.Date     `yaml:"to"`
	STCGRate decimal.Decimal `yaml:"stcg_rate"`
	LTCGRate decimal.Decimal `yaml:"ltcg_rate"`
}

// Label is the stable identifier of the period ("2024-07-23..2025-03-31").
func (p RatePeriod) Label() string { return p.From.String() + ".." + p.To.String() }

// Contains reports whether d is inside the period (inclusive).
func (p RatePeriod) Contains(d domain.Date) bool { return !d.Before(p.From) && !d.After(p.To) }

// EquityRules cover STT-paid listed equity and equity-oriented mutual fund units.
type EquityRules struct {
	LongTermAfterMonths     int             `yaml:"long_term_after_months"`
	LongTermBoundary        string          `yaml:"long_term_boundary"` // on_or_after_anniversary | after_anniversary
	Periods                 []RatePeriod    `yaml:"periods"`
	LTCGExemption           decimal.Decimal `yaml:"ltcg_exemption"`
	LTCGExemptionAllocation string          `yaml:"ltcg_exemption_allocation"` // pro_rata | earliest_first | highest_rate_first
	ReportSplitByDate       domain.Date     `yaml:"report_split_by_date"`
	Rebate87AOnLTCG         bool            `yaml:"rebate_87a_on_ltcg"`
}

// GrandfatheringRules implement s.55(2)(ac) / s.90(7).
type GrandfatheringRules struct {
	Enabled               bool        `yaml:"enabled"`
	CutoffAcquisitionDate domain.Date `yaml:"cutoff_acquisition_date"`
	FMVDate               domain.Date `yaml:"fmv_date"`
	ApplyToBonusShares    bool        `yaml:"apply_to_bonus_shares"`
}

// IntradayRules control same-day netting.
type IntradayRules struct {
	Netting string `yaml:"netting"` // min_buy_sell_per_day | none
}

// BuybackPeriod maps a date range to a buyback tax regime.
type BuybackPeriod struct {
	From   domain.Date `yaml:"from"`
	To     domain.Date `yaml:"to"`
	Regime string      `yaml:"regime"` // exempt_115qa | deemed_dividend | capital_gains
}

// BuybackRules hold the dated regimes.
type BuybackRules struct {
	Periods []BuybackPeriod `yaml:"periods"`
}

// RegimeOn returns the regime for a buyback on date d ("" if none configured).
func (b BuybackRules) RegimeOn(d domain.Date) string {
	for _, p := range b.Periods {
		if !d.Before(p.From) && !d.After(p.To) {
			return p.Regime
		}
	}
	return ""
}

// FNORules cover turnover and the audit thresholds that are shown (never decided) to the user.
type FNORules struct {
	TurnoverMethod        string          `yaml:"turnover_method"`
	AuditThreshold        decimal.Decimal `yaml:"audit_threshold"`
	AuditThresholdLowCash decimal.Decimal `yaml:"audit_threshold_low_cash"`
	AuditLowCashPct       decimal.Decimal `yaml:"audit_low_cash_pct"`
}

// LossRule describes set-off and carry-forward for one loss type.
type LossRule struct {
	Loss                string   `yaml:"loss"`
	SetOffAgainst       []string `yaml:"set_off_against"`
	CarryForwardYears   int      `yaml:"carry_forward_years"`
	CarryForwardAgainst []string `yaml:"carry_forward_against"`
}

// Rules is one financial year's complete rule set.
type Rules struct {
	Year           string              `yaml:"year"`
	Law            string              `yaml:"law"`
	YearLabel      string              `yaml:"year_label"`
	Version        int                 `yaml:"version"`
	Equity         EquityRules         `yaml:"equity"`
	Grandfathering GrandfatheringRules `yaml:"grandfathering"`
	Intraday       IntradayRules       `yaml:"intraday"`
	Buyback        BuybackRules        `yaml:"buyback"`
	FNO            FNORules            `yaml:"fno"`
	Losses         []LossRule          `yaml:"losses"`
	References     map[string]string   `yaml:"references"`
	Sources        []string            `yaml:"sources"`
	VerifiedBy     string              `yaml:"verified_by"`
	file           string
}

// VersionString is what every job records, e.g. "fy2025-26@1" or "ty2026-27@1".
func (r *Rules) VersionString() string {
	return fmt.Sprintf("%s@%d", strings.TrimSuffix(r.file, ".yaml"), r.Version)
}

// IsNewAct reports whether the year falls under the Income-tax Act, 2025.
func (r *Rules) IsNewAct() bool { return strings.Contains(r.Law, "2025") }

// PeriodFor returns the rate period containing the sale date.
func (r *Rules) PeriodFor(sale domain.Date) (RatePeriod, bool) {
	for _, p := range r.Equity.Periods {
		if p.Contains(sale) {
			return p, true
		}
	}
	return RatePeriod{}, false
}

// Bounds returns the first and last day of the year.
func (r *Rules) Bounds() (domain.Date, domain.Date) {
	a, b, _ := domain.FinancialYearBounds(r.Year)
	return a, b
}

var (
	loadOnce sync.Once
	loaded   map[string]*Rules
	loadErr  error
)

func loadAll() {
	loaded = map[string]*Rules{}
	entries, err := fs.ReadDir(rulesFS, "rules")
	if err != nil {
		loadErr = err
		return
	}
	for _, e := range entries {
		b, err := rulesFS.ReadFile("rules/" + e.Name())
		if err != nil {
			loadErr = err
			return
		}
		var r Rules
		if err := yaml.Unmarshal(b, &r); err != nil {
			loadErr = fmt.Errorf("rules %s: %w", e.Name(), err)
			return
		}
		r.file = e.Name()
		if err := r.validate(); err != nil {
			loadErr = fmt.Errorf("rules %s: %w", e.Name(), err)
			return
		}
		loaded[r.Year] = &r
	}
}

func (r *Rules) validate() error {
	if r.Year == "" || r.Law == "" || r.YearLabel == "" {
		return fmt.Errorf("year, law and year_label are required")
	}
	if _, _, err := domain.FinancialYearBounds(r.Year); err != nil {
		return err
	}
	if r.Equity.LongTermAfterMonths <= 0 {
		return fmt.Errorf("equity.long_term_after_months must be positive")
	}
	switch r.Equity.LongTermBoundary {
	case "on_or_after_anniversary", "after_anniversary":
	default:
		return fmt.Errorf("equity.long_term_boundary %q unknown", r.Equity.LongTermBoundary)
	}
	first, last := r.Bounds()
	if len(r.Equity.Periods) == 0 {
		return fmt.Errorf("equity.periods must not be empty")
	}
	cursor := first
	for i, p := range r.Equity.Periods {
		if !p.From.Equal(cursor) {
			return fmt.Errorf("equity.periods[%d] starts %s, expected %s (periods must tile the year)", i, p.From, cursor)
		}
		if p.To.Before(p.From) {
			return fmt.Errorf("equity.periods[%d] ends before it starts", i)
		}
		cursor = p.To.AddDays(1)
	}
	if !cursor.Equal(last.AddDays(1)) {
		return fmt.Errorf("equity.periods end at %s, expected %s", cursor.AddDays(-1), last)
	}
	if r.Grandfathering.Enabled && r.Grandfathering.CutoffAcquisitionDate.IsZero() {
		return fmt.Errorf("grandfathering.cutoff_acquisition_date required")
	}
	return nil
}

// Load returns the rules for a financial year label such as "2025-26".
func Load(fy string) (*Rules, error) {
	loadOnce.Do(loadAll)
	if loadErr != nil {
		return nil, loadErr
	}
	r, ok := loaded[fy]
	if !ok {
		return nil, fmt.Errorf("no rules file for year %q (available: %s)", fy, strings.Join(Years(), ", "))
	}
	return r, nil
}

// LoadForDate returns the rules for the year containing d.
func LoadForDate(d domain.Date) (*Rules, error) { return Load(domain.FinancialYear(d)) }

// Years lists the years that have rules, sorted.
func Years() []string {
	loadOnce.Do(loadAll)
	ys := make([]string, 0, len(loaded))
	for y := range loaded {
		ys = append(ys, y)
	}
	sort.Strings(ys)
	return ys
}

// IsLongTerm applies the holding-period rule. The boundary is configurable because the
// statute says "held for more than N months immediately preceding the date of transfer":
//   - on_or_after_anniversary: Delhi HC (Bharti Gupta Ramola, 2012) — the acquisition day is
//     counted, so 12 months complete the day before the anniversary and a sale on the
//     anniversary is long-term. This is the default.
//   - after_anniversary: the stricter reading some broker tools use (sale must be strictly later).
func IsLongTerm(acq, sale domain.Date, r *Rules) bool {
	anniversary := acq.AddMonths(r.Equity.LongTermAfterMonths)
	if r.Equity.LongTermBoundary == "after_anniversary" {
		return sale.After(anniversary)
	}
	return !sale.Before(anniversary)
}
