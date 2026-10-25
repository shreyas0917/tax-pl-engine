package tax

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/matcher"
)

// Buyback regimes (values used in the rules files).
const (
	BuybackExempt115QA    = "exempt_115qa"
	BuybackDeemedDividend = "deemed_dividend"
	BuybackCapitalGains   = "capital_gains"
)

// Classification is the output of Classify: every gain line plus the items it could not place.
type Classification struct {
	Gains  []domain.Gain       `json:"gains"`
	Review []domain.ReviewItem `json:"review"`
	// YearsUsed maps FY -> rules version string, for the report header.
	YearsUsed map[string]string `json:"years_used"`
}

// Classify turns matcher output into classified gains. Each sale is classified under the
// rules of the financial year of its trade date. Sales in years without a rules file become
// review items rather than silently dropped.
func Classify(res matcher.Result) Classification {
	out := Classification{YearsUsed: map[string]string{}}
	rulesFor := func(d domain.Date) (*Rules, bool) {
		fy := domain.FinancialYear(d)
		r, err := Load(fy)
		if err != nil {
			id := deriveID("review-year", fy)
			if !hasReview(out.Review, id) {
				out.Review = append(out.Review, domain.ReviewItem{ID: id, Kind: domain.ReviewParseIssue,
					Message: fmt.Sprintf("No tax rules are configured for %s; sales in that year were not classified.", fy)})
			}
			return nil, false
		}
		out.YearsUsed[fy] = r.VersionString()
		return r, true
	}

	for _, m := range res.Matches {
		if m.SellKind == domain.KindTransferOut {
			continue // not a transfer under s.2(47); no gain
		}
		r, ok := rulesFor(m.SaleDate)
		if !ok {
			continue
		}
		out.Gains = append(out.Gains, classifyMatch(m, r))
	}
	for _, m := range res.Intraday {
		r, ok := rulesFor(m.Date)
		if !ok {
			continue
		}
		g := domain.Gain{
			ID: deriveID("gain", m.ID), FY: r.Year, Category: domain.CatIntraday, AccountID: m.AccountID, ISIN: m.ISIN, Symbol: m.Symbol,
			IntradayMatchID: m.ID, SaleDate: m.Date, AcquisitionDate: m.Date, Quantity: m.Quantity,
			SaleValue: m.SellValue, ActualCost: m.BuyValue, CostOfAcquisition: m.BuyValue, ChargesDeductible: m.Charges,
			RulesVersion: r.VersionString(),
		}
		g.Gain = g.SaleValue.Sub(g.CostOfAcquisition).Sub(g.ChargesDeductible)
		g.Steps = []string{
			fmt.Sprintf("Bought and sold %s units of %s on %s in account %s", m.Quantity, m.Symbol, m.Date, m.AccountID),
			fmt.Sprintf("Same-day squared-off quantity is speculative business income (%s %s)", r.Law, r.References["speculative"]),
			fmt.Sprintf("Gain = sale %s - buy %s - charges %s (all charges incl. STT deductible) = %s", g.SaleValue, g.CostOfAcquisition, g.ChargesDeductible, g.Gain),
		}
		out.Gains = append(out.Gains, g)
	}
	for _, m := range res.FNO {
		r, ok := rulesFor(m.CloseDate)
		if !ok {
			continue
		}
		pnl := m.PnL()
		g := domain.Gain{
			ID: deriveID("gain", m.ID), FY: r.Year, Category: domain.CatFNO, AccountID: m.AccountID, Symbol: m.Symbol, ContractKey: m.ContractKey,
			FNOMatchID: m.ID, SaleDate: m.CloseDate, AcquisitionDate: m.OpenDate, Quantity: m.Quantity,
			ChargesDeductible: m.Charges, RulesVersion: r.VersionString(),
		}
		if m.OpenSide == domain.Buy {
			g.SaleValue, g.ActualCost = m.CloseValue, m.OpenValue
		} else {
			g.SaleValue, g.ActualCost = m.OpenValue, m.CloseValue
		}
		g.CostOfAcquisition = g.ActualCost
		g.Gain = pnl.Sub(m.Charges)
		side := "long"
		if m.OpenSide == domain.Sell {
			side = "short"
		}
		g.Steps = []string{
			fmt.Sprintf("Opened %s %s units of %s on %s, closed on %s (FIFO within account %s)", side, m.Quantity, m.ContractKey, m.OpenDate, m.CloseDate, m.AccountID),
			"Futures and options are non-speculative business income",
			fmt.Sprintf("P&L = %s - charges %s = %s; |P&L| %s counts towards turnover", pnl, m.Charges, g.Gain, pnl.Abs()),
		}
		out.Gains = append(out.Gains, g)
	}
	sort.SliceStable(out.Gains, func(i, j int) bool {
		a, b := out.Gains[i], out.Gains[j]
		if c := a.SaleDate.Compare(b.SaleDate); c != 0 {
			return c < 0
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		if c := a.AcquisitionDate.Compare(b.AcquisitionDate); c != 0 {
			return c < 0
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.ID < b.ID
	})
	return out
}

func classifyMatch(m domain.LotMatch, r *Rules) domain.Gain {
	g := domain.Gain{
		ID: deriveID("gain", m.ID), FY: r.Year, AccountID: m.AccountID, ISIN: m.ISIN, Symbol: m.Symbol, LotMatchID: m.ID,
		SaleDate: m.SaleDate, AcquisitionDate: m.AcquisitionDate, Quantity: m.Quantity, SaleValue: m.SaleValue,
		ActualCost: m.CostBasis, CostOfAcquisition: m.CostBasis, ChargesDeductible: m.SaleCharges, HoldingDays: m.HoldingDays,
		RulesVersion: r.VersionString(),
	}
	g.Steps = append(g.Steps, fmt.Sprintf("Matched %s units FIFO in account %s against lot acquired %s (%s)", m.Quantity, m.AccountID, m.AcquisitionDate, strings.ToLower(string(m.LotOrigin))))
	for _, a := range m.Adjustments {
		g.Steps = append(g.Steps, fmt.Sprintf("%s %s on %s: %s -> %s units", a.Kind, a.Ratio, a.ExDate, a.QtyFrom, a.QtyTo))
	}

	// Same-day squared-off quantity reaching here means netting was disabled: still speculative.
	if m.SaleDate.Equal(m.AcquisitionDate) && m.LotOrigin == domain.OriginTrade && m.SellKind != domain.KindBuyback {
		g.Category = domain.CatIntraday
		g.ChargesDeductible = m.SaleCharges // buy-side charges are already in the cost
		g.Gain = g.SaleValue.Sub(g.CostOfAcquisition).Sub(g.ChargesDeductible)
		g.Steps = append(g.Steps, "Bought and sold on the same day: speculative business income")
		return g
	}

	longTerm := IsLongTerm(m.AcquisitionDate, m.SaleDate, r)
	if longTerm {
		g.Category = domain.CatLTCGEquity
		g.Steps = append(g.Steps, fmt.Sprintf("Held %d days, from %s to %s: at least %d months, so long-term (%s %s)", m.HoldingDays, m.AcquisitionDate, m.SaleDate, r.Equity.LongTermAfterMonths, r.Law, r.References["ltcg"]))
	} else {
		g.Category = domain.CatSTCGEquity
		g.Steps = append(g.Steps, fmt.Sprintf("Held %d days, from %s to %s: less than %d months, so short-term (%s %s)", m.HoldingDays, m.AcquisitionDate, m.SaleDate, r.Equity.LongTermAfterMonths, r.Law, r.References["stcg"]))
	}
	if p, ok := r.PeriodFor(m.SaleDate); ok {
		g.RatePeriod = p.Label()
	}

	if longTerm && r.Grandfathering.Enabled && m.GrandfatheredFMV != nil && !m.AcquisitionDate.After(r.Grandfathering.CutoffAcquisitionDate) &&
		(m.LotOrigin != domain.OriginBonus || r.Grandfathering.ApplyToBonusShares) {
		fmvTotal := domain.RoundMoney(m.GrandfatheredFMV.Mul(m.Quantity))
		chosen := domain.Max(m.CostBasis, domain.Min(fmvTotal, m.SaleValue))
		gf := &domain.Grandfathering{FMVPerUnit: *m.GrandfatheredFMV, FMVTotal: fmvTotal, SaleValue: m.SaleValue, ActualCost: m.CostBasis, ChosenCost: chosen, Applied: !chosen.Equal(m.CostBasis)}
		switch {
		case chosen.Equal(m.CostBasis):
			gf.Explanation = "actual cost is higher than min(FMV, sale value); actual cost used"
		case chosen.Equal(fmvTotal):
			gf.Explanation = "FMV on 31 Jan 2018 is below the sale value and above actual cost; FMV used as cost"
		default:
			gf.Explanation = "sale value is below FMV and above actual cost; sale value used as cost (no gain, no loss)"
		}
		g.Grandfathering = gf
		g.CostOfAcquisition = chosen
		g.Steps = append(g.Steps, fmt.Sprintf("Acquired on or before %s: grandfathering (%s). Cost = max(actual %s, min(FMV %s x %s = %s, sale %s)) = %s",
			r.Grandfathering.CutoffAcquisitionDate, r.References["grandfathering"], m.CostBasis, m.GrandfatheredFMV.StringFixed(4), m.Quantity, fmvTotal, m.SaleValue, chosen))
	}

	if m.SellKind == domain.KindBuyback {
		regime := r.Buyback.RegimeOn(m.SaleDate)
		g.BuybackRegime = regime
		switch regime {
		case BuybackCapitalGains:
			g.Steps = append(g.Steps, fmt.Sprintf("Buyback on %s: taxed as capital gains (%s %s)", m.SaleDate, r.Law, r.References["buyback"]))
		case BuybackDeemedDividend:
			// s.2(22)(f): the whole consideration is dividend income; s.46A: consideration for
			// capital-gains purposes is nil, so the cost becomes a capital loss.
			g.Category = domain.CatBuyback
			g.BuybackProceeds = m.SaleValue
			g.SaleValue = domain.Zero
			g.Gain = domain.Zero.Sub(g.CostOfAcquisition).Sub(g.ChargesDeductible)
			g.Steps = append(g.Steps, fmt.Sprintf("Buyback on %s falls under the deemed-dividend regime (%s): proceeds %s are dividend income (other sources); for capital gains the consideration is nil so the cost %s is a %s capital loss",
				m.SaleDate, r.References["buyback"], m.SaleValue, g.CostOfAcquisition, map[bool]string{true: "long-term", false: "short-term"}[longTerm]))
			return g
		case BuybackExempt115QA:
			g.Category = domain.CatBuyback
			g.BuybackProceeds = m.SaleValue
			g.Gain = domain.Zero
			g.Steps = append(g.Steps, fmt.Sprintf("Buyback on %s: the company paid buyback tax under s.115QA; proceeds %s are exempt in your hands (s.10(34A)), shown for information", m.SaleDate, m.SaleValue))
			return g
		default:
			g.Steps = append(g.Steps, "Buyback: no regime configured for this date; treated as an ordinary sale")
		}
	}

	g.Gain = g.SaleValue.Sub(g.CostOfAcquisition).Sub(g.ChargesDeductible)
	g.Steps = append(g.Steps, fmt.Sprintf("Gain = sale %s - cost %s - sale charges %s (STT not deductible) = %s", g.SaleValue, g.CostOfAcquisition, g.ChargesDeductible, g.Gain))
	return g
}

func hasReview(items []domain.ReviewItem, id string) bool {
	for _, it := range items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func deriveID(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// ConfigFor derives the matcher configuration from a year's rules.
func ConfigFor(r *Rules) matcher.Config {
	c := matcher.Config{IntradayNetting: r.Intraday.Netting == "min_buy_sell_per_day"}
	if r.Grandfathering.Enabled {
		c.GrandfatheringCutoff = r.Grandfathering.CutoffAcquisitionDate
	}
	return c
}
