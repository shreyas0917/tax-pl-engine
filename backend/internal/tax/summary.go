package tax

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
)

// PeriodTotal is the LTCG/STCG total within one rate period (FY 2024-25 has two).
type PeriodTotal struct {
	Period   string          `json:"period"`
	STCGRate decimal.Decimal `json:"stcg_rate"`
	LTCGRate decimal.Decimal `json:"ltcg_rate"`
	STCG     decimal.Decimal `json:"stcg"`
	LTCG     decimal.Decimal `json:"ltcg"`
	// Estimate fields (labelled "estimate" everywhere they are shown).
	LTCGExemptionAllocated decimal.Decimal `json:"ltcg_exemption_allocated"`
	LTCGTaxable            decimal.Decimal `json:"ltcg_taxable"`
	EstimatedTaxSTCG       decimal.Decimal `json:"estimated_tax_stcg"`
	EstimatedTaxLTCG       decimal.Decimal `json:"estimated_tax_ltcg"`
}

// CategoryTotal is a per-category roll-up.
type CategoryTotal struct {
	Category  domain.Category `json:"category"`
	Count     int             `json:"count"`
	SaleValue decimal.Decimal `json:"sale_value"`
	Cost      decimal.Decimal `json:"cost"`
	Charges   decimal.Decimal `json:"charges"`
	Gain      decimal.Decimal `json:"gain"`
}

// AccountTotal is a per-account, per-category roll-up.
type AccountTotal struct {
	AccountID string                              `json:"account_id"`
	ByCat     map[domain.Category]decimal.Decimal `json:"by_category"`
}

// LossLine shows what a net loss in a category can be set off against and carried forward.
type LossLine struct {
	Loss                string          `json:"loss"`
	Amount              decimal.Decimal `json:"amount"`
	SetOffAgainst       []string        `json:"set_off_against"`
	CarryForwardYears   int             `json:"carry_forward_years"`
	CarryForwardAgainst []string        `json:"carry_forward_against"`
}

// Summary is one financial year's totals.
type Summary struct {
	FY           string `json:"fy"`
	YearLabel    string `json:"year_label"`
	Law          string `json:"law"`
	RulesVersion string `json:"rules_version"`
	VerifiedBy   string `json:"verified_by"`

	Categories []CategoryTotal `json:"categories"`
	Accounts   []AccountTotal  `json:"accounts"`
	Periods    []PeriodTotal   `json:"periods"`

	STCG     decimal.Decimal `json:"stcg"`
	LTCG     decimal.Decimal `json:"ltcg"`
	Intraday decimal.Decimal `json:"intraday"`
	FNO      decimal.Decimal `json:"fno"`
	// BuybackDividend is the deemed-dividend income (other sources) from buybacks in the
	// 1 Oct 2024 - 31 Mar 2026 window; BuybackExempt is s.115QA-era exempt proceeds.
	BuybackDividend decimal.Decimal `json:"buyback_dividend"`
	BuybackExempt   decimal.Decimal `json:"buyback_exempt"`

	// F&O turnover per ICAI Guidance Note: sum of absolute P&L per closed position.
	FNOTurnover            decimal.Decimal   `json:"fno_turnover"`
	IntradayTurnover       decimal.Decimal   `json:"intraday_turnover"`
	AuditThreshold         decimal.Decimal   `json:"audit_threshold"`
	AuditThresholdLowCash  decimal.Decimal   `json:"audit_threshold_low_cash"`
	AuditNote              string            `json:"audit_note"`
	LTCGExemption          decimal.Decimal   `json:"ltcg_exemption"`
	LTCGTaxableAfterExempt decimal.Decimal   `json:"ltcg_taxable_after_exemption"`
	EstimatedTax           decimal.Decimal   `json:"estimated_tax"`
	EstimateNotes          []string          `json:"estimate_notes"`
	Losses                 []LossLine        `json:"losses"`
	References             map[string]string `json:"references"`
}

// Summarise builds a Summary for one year from classified gains.
func Summarise(fy string, gains []domain.Gain) (Summary, error) {
	r, err := Load(fy)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{FY: fy, YearLabel: r.YearLabel, Law: r.Law, RulesVersion: r.VersionString(), VerifiedBy: r.VerifiedBy,
		AuditThreshold: r.FNO.AuditThreshold, AuditThresholdLowCash: r.FNO.AuditThresholdLowCash, LTCGExemption: r.Equity.LTCGExemption,
		References: r.References}
	cats := map[domain.Category]*CategoryTotal{}
	accts := map[string]*AccountTotal{}
	periods := map[string]*PeriodTotal{}
	for _, p := range r.Equity.Periods {
		periods[p.Label()] = &PeriodTotal{Period: p.Label(), STCGRate: p.STCGRate, LTCGRate: p.LTCGRate}
	}
	for _, g := range gains {
		if g.FY != fy {
			continue
		}
		c := cats[g.Category]
		if c == nil {
			c = &CategoryTotal{Category: g.Category}
			cats[g.Category] = c
		}
		c.Count++
		c.SaleValue = c.SaleValue.Add(g.SaleValue)
		c.Cost = c.Cost.Add(g.CostOfAcquisition)
		c.Charges = c.Charges.Add(g.ChargesDeductible)
		c.Gain = c.Gain.Add(g.Gain)

		a := accts[g.AccountID]
		if a == nil {
			a = &AccountTotal{AccountID: g.AccountID, ByCat: map[domain.Category]decimal.Decimal{}}
			accts[g.AccountID] = a
		}
		a.ByCat[g.Category] = a.ByCat[g.Category].Add(g.Gain)

		switch g.Category {
		case domain.CatSTCGEquity:
			s.STCG = s.STCG.Add(g.Gain)
			if p := periods[g.RatePeriod]; p != nil {
				p.STCG = p.STCG.Add(g.Gain)
			}
		case domain.CatLTCGEquity:
			s.LTCG = s.LTCG.Add(g.Gain)
			if p := periods[g.RatePeriod]; p != nil {
				p.LTCG = p.LTCG.Add(g.Gain)
			}
		case domain.CatIntraday:
			s.Intraday = s.Intraday.Add(g.Gain)
			s.IntradayTurnover = s.IntradayTurnover.Add(g.Gain.Abs())
		case domain.CatFNO:
			s.FNO = s.FNO.Add(g.Gain)
			s.FNOTurnover = s.FNOTurnover.Add(g.SaleValue.Sub(g.ActualCost).Abs())
		case domain.CatBuyback:
			switch g.BuybackRegime {
			case BuybackDeemedDividend:
				// The gain line carries the capital loss; the dividend is the original sale value,
				// which the classifier zeroed on the gain. Recover it from the cost + gain identity.
				s.BuybackDividend = s.BuybackDividend.Add(buybackProceeds(g))
				if g.Gain.IsNegative() {
					if IsLongTerm(g.AcquisitionDate, g.SaleDate, r) {
						s.LTCG = s.LTCG.Add(g.Gain)
					} else {
						s.STCG = s.STCG.Add(g.Gain)
					}
				}
			case BuybackExempt115QA:
				s.BuybackExempt = s.BuybackExempt.Add(buybackProceeds(g))
			}
		}
	}
	for _, c := range cats {
		s.Categories = append(s.Categories, *c)
	}
	sort.Slice(s.Categories, func(i, j int) bool { return s.Categories[i].Category < s.Categories[j].Category })
	for _, a := range accts {
		s.Accounts = append(s.Accounts, *a)
	}
	sort.Slice(s.Accounts, func(i, j int) bool { return s.Accounts[i].AccountID < s.Accounts[j].AccountID })
	for _, p := range r.Equity.Periods {
		s.Periods = append(s.Periods, *periods[p.Label()])
	}

	s.estimate(r)
	s.losses(r)
	s.AuditNote = fmt.Sprintf("Tax audit (%s %s) applies above a turnover of %s, or %s when cash receipts and payments are each at most %s%% of the total. This tool does not decide whether an audit is needed; check with your CA.",
		r.Law, r.References["audit"], inr(r.FNO.AuditThreshold), inr(r.FNO.AuditThresholdLowCash), r.FNO.AuditLowCashPct.Mul(decimal.NewFromInt(100)))
	return s, nil
}

func buybackProceeds(g domain.Gain) decimal.Decimal { return g.BuybackProceeds }

// estimate computes the estimated tax on equity gains. It is deliberately simple: it applies
// the year's rates to STCG and LTCG after the LTCG exemption, ignores other income, surcharge,
// cess, set-off ordering and the s.87A rebate (not available on s.112A gains). The UI labels
// it "estimate" everywhere.
func (s *Summary) estimate(r *Rules) {
	s.EstimateNotes = append(s.EstimateNotes,
		"Estimate only: applies the year's STCG/LTCG rates after the LTCG exemption; ignores other income, surcharge, cess, loss set-off and carry-forward.")
	if !r.Equity.Rebate87AOnLTCG {
		s.EstimateNotes = append(s.EstimateNotes, "Long-term gains on STT-paid equity are not eligible for the s.87A rebate, so tax can be payable even when total income is low.")
	}
	ltcgPositive := domain.Max(s.LTCG, domain.Zero)
	exempt := domain.Min(ltcgPositive, r.Equity.LTCGExemption)
	s.LTCGTaxableAfterExempt = ltcgPositive.Sub(exempt)
	total := domain.Zero
	// Allocate the exemption across periods (matters only for FY 2024-25).
	var positive []*PeriodTotal
	sumPos := domain.Zero
	for i := range s.Periods {
		if s.Periods[i].LTCG.IsPositive() {
			positive = append(positive, &s.Periods[i])
			sumPos = sumPos.Add(s.Periods[i].LTCG)
		}
	}
	remaining := exempt
	switch r.Equity.LTCGExemptionAllocation {
	case "highest_rate_first":
		sort.SliceStable(positive, func(i, j int) bool { return positive[i].LTCGRate.GreaterThan(positive[j].LTCGRate) })
		fallthrough
	case "earliest_first":
		for _, p := range positive {
			p.LTCGExemptionAllocated = domain.Min(p.LTCG, remaining)
			remaining = remaining.Sub(p.LTCGExemptionAllocated)
		}
	default: // pro_rata
		for i, p := range positive {
			if i == len(positive)-1 {
				p.LTCGExemptionAllocated = remaining
			} else {
				p.LTCGExemptionAllocated = domain.ProRata(exempt, p.LTCG, sumPos)
				remaining = remaining.Sub(p.LTCGExemptionAllocated)
			}
		}
		if len(positive) > 1 {
			s.EstimateNotes = append(s.EstimateNotes, "The single annual LTCG exemption was allocated pro-rata between the two rate periods; the ITR utility may absorb it in a different order.")
		}
	}
	for i := range s.Periods {
		p := &s.Periods[i]
		p.LTCGTaxable = domain.Max(p.LTCG.Sub(p.LTCGExemptionAllocated), domain.Zero)
		p.EstimatedTaxLTCG = domain.RoundMoney(p.LTCGTaxable.Mul(p.LTCGRate))
		p.EstimatedTaxSTCG = domain.RoundMoney(domain.Max(p.STCG, domain.Zero).Mul(p.STCGRate))
		total = total.Add(p.EstimatedTaxLTCG).Add(p.EstimatedTaxSTCG)
	}
	s.EstimatedTax = total
}

// losses lists net losses per bucket with their set-off and carry-forward rules from the rules file.
func (s *Summary) losses(r *Rules) {
	add := func(loss string, amount decimal.Decimal) {
		if !amount.IsNegative() {
			return
		}
		for _, lr := range r.Losses {
			if lr.Loss == loss {
				s.Losses = append(s.Losses, LossLine{Loss: loss, Amount: amount.Abs(), SetOffAgainst: lr.SetOffAgainst, CarryForwardYears: lr.CarryForwardYears, CarryForwardAgainst: lr.CarryForwardAgainst})
			}
		}
	}
	add("STCL", s.STCG)
	add("LTCL", s.LTCG)
	add("SPECULATIVE", s.Intraday)
	add("NON_SPECULATIVE_BUSINESS", s.FNO)
	if len(s.Losses) > 0 {
		s.EstimateNotes = append(s.EstimateNotes, "Losses can be carried forward only if the return is filed by the due date.")
	}
}

func inr(d decimal.Decimal) string {
	// Indian grouping: last three digits, then pairs.
	whole := d.Truncate(0).Abs().String()
	if len(whole) <= 3 {
		return "Rs " + whole
	}
	last3 := whole[len(whole)-3:]
	rest := whole[:len(whole)-3]
	var parts []string
	for len(rest) > 2 {
		parts = append([]string{rest[len(rest)-2:]}, parts...)
		rest = rest[:len(rest)-2]
	}
	if rest != "" {
		parts = append([]string{rest}, parts...)
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return "Rs " + out + "," + last3
}
