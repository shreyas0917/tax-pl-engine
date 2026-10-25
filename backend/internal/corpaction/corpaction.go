// Package corpaction applies corporate actions (split, bonus, demerger, merger) to open lots.
// It is pure and is called by the matcher on each action's ex-date, before that day's trades.
package corpaction

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
)

// Result of applying one action to one account's lots.
type Result struct {
	NewLots []*domain.Lot // bonus / demerger / merger lots created
	Notes   []string      // audit lines, one per adjusted lot
	// MovedToISIN is set for MERGER: the old-ISIN lots were closed and re-created under NewISIN.
	MovedToISIN string
}

// Apply mutates the open lots (QuantityOpen > 0) of the action's ISIN in place and returns any
// lots it created. Lots with no open quantity are untouched: they were sold before the ex-date.
func Apply(a domain.CorporateAction, open []*domain.Lot) (Result, error) {
	var res Result
	if a.RatioFrom.IsZero() || a.RatioTo.IsZero() {
		return res, fmt.Errorf("corporate action %s %s: ratio must be non-zero", a.Kind, a.ISIN)
	}
	factor := a.RatioTo.Div(a.RatioFrom) // new units per old unit
	for _, lot := range open {
		if lot.ISIN != a.ISIN || !lot.QuantityOpen.IsPositive() {
			continue
		}
		switch a.Kind {
		case domain.CASplit:
			applySplit(a, factor, lot, &res)
		case domain.CABonus:
			applyBonus(a, factor, lot, &res)
		case domain.CADemerger:
			if a.NewISIN == "" {
				return res, fmt.Errorf("demerger of %s on %s: new_isin required", a.ISIN, a.ExDate)
			}
			applyDemerger(a, factor, lot, &res)
		case domain.CAMerger:
			if a.NewISIN == "" {
				return res, fmt.Errorf("merger of %s on %s: new_isin required", a.ISIN, a.ExDate)
			}
			applyMerger(a, factor, lot, &res)
			res.MovedToISIN = a.NewISIN
		default:
			return res, fmt.Errorf("unknown corporate action kind %q", a.Kind)
		}
	}
	return res, nil
}

// applySplit: quantity x factor, total cost unchanged, acquisition date unchanged,
// grandfathered FMV per unit / factor so that total FMV is unchanged (spec 6.5).
func applySplit(a domain.CorporateAction, factor decimal.Decimal, lot *domain.Lot, res *Result) {
	adj := domain.Adjustment{Kind: string(domain.CASplit), ExDate: a.ExDate, Ratio: a.RatioString(),
		QtyFrom: lot.QuantityOpen, CostFrom: lot.CostOpen, CostTo: lot.CostOpen}
	lot.QuantityOpen = domain.RoundQty(lot.QuantityOpen.Mul(factor))
	lot.QuantityOriginal = domain.RoundQty(lot.QuantityOriginal.Mul(factor))
	adj.QtyTo = lot.QuantityOpen
	if lot.GrandfatheredFMV != nil {
		fmv := lot.GrandfatheredFMV.Div(factor)
		lot.GrandfatheredFMV = &fmv
		adj.Note = fmt.Sprintf("FMV per unit adjusted to %s so total 31-Jan-2018 FMV is unchanged", fmv.StringFixed(4))
	}
	lot.Adjustments = append(lot.Adjustments, adj)
	res.Notes = append(res.Notes, fmt.Sprintf("SPLIT %s on %s: lot %s %s -> %s units, cost unchanged %s",
		a.RatioString(), a.ExDate, lot.ID, adj.QtyFrom, adj.QtyTo, lot.CostOpen))
}

// applyBonus: a new lot of bonus units with cost 0 and acquisition date = ex-date. The original
// lot is unchanged (its holding period and cost stay with it). Fractional entitlements are kept
// as fractional quantities; companies usually pay cash for fractions, which the user records
// as a sale if material.
func applyBonus(a domain.CorporateAction, factor decimal.Decimal, lot *domain.Lot, res *Result) {
	qty := domain.RoundQty(lot.QuantityOpen.Mul(factor))
	if !qty.IsPositive() {
		return
	}
	nl := &domain.Lot{
		ID:               deriveID("bonus", lot.ID, a.ID, a.ExDate.String()),
		AccountID:        lot.AccountID,
		ISIN:             lot.ISIN,
		Symbol:           lot.Symbol,
		OpenTradeID:      lot.ID,
		Origin:           domain.OriginBonus,
		QuantityOriginal: qty,
		QuantityOpen:     qty,
		CostOriginal:     domain.Zero,
		CostOpen:         domain.Zero,
		AcquisitionDate:  a.ExDate,
		Sequence:         lot.Sequence,
		Adjustments: []domain.Adjustment{{Kind: string(domain.CABonus), ExDate: a.ExDate, Ratio: a.RatioString(),
			QtyFrom: lot.QuantityOpen, QtyTo: qty, CostFrom: domain.Zero, CostTo: domain.Zero,
			Note: "bonus units: cost nil, holding period from allotment (ex) date"}},
	}
	res.NewLots = append(res.NewLots, nl)
	res.Notes = append(res.Notes, fmt.Sprintf("BONUS %s on %s: lot %s (%s units) -> new lot %s of %s units at nil cost",
		a.RatioString(), a.ExDate, lot.ID, lot.QuantityOpen, nl.ID, qty))
}

// applyDemerger: s.49(2C)/(2D). CostSplitPct of the original cost moves to a new lot in the
// resulting company's ISIN; the original lot keeps the rest. Acquisition date carries over
// to the new lot (s.2(42A) Explanation 1(g)).
func applyDemerger(a domain.CorporateAction, factor decimal.Decimal, lot *domain.Lot, res *Result) {
	moved := domain.RoundMoney(lot.CostOpen.Mul(a.CostSplitPct))
	movedOriginal := domain.RoundMoney(lot.CostOriginal.Mul(a.CostSplitPct))
	qty := domain.RoundQty(lot.QuantityOpen.Mul(factor))
	adj := domain.Adjustment{Kind: string(domain.CADemerger), ExDate: a.ExDate, Ratio: a.RatioString(),
		QtyFrom: lot.QuantityOpen, QtyTo: lot.QuantityOpen, CostFrom: lot.CostOpen, CostTo: lot.CostOpen.Sub(moved),
		Note: fmt.Sprintf("%s%% of cost moved to %s under s.49(2C)", a.CostSplitPct.Mul(decimal.NewFromInt(100)).String(), a.NewISIN)}
	lot.CostOpen = lot.CostOpen.Sub(moved)
	lot.CostOriginal = lot.CostOriginal.Sub(movedOriginal)
	lot.Adjustments = append(lot.Adjustments, adj)
	nl := &domain.Lot{
		ID:               deriveID("demerger", lot.ID, a.ID, a.NewISIN),
		AccountID:        lot.AccountID,
		ISIN:             a.NewISIN,
		Symbol:           a.NewSymbol,
		OpenTradeID:      lot.ID,
		Origin:           domain.OriginDemerger,
		QuantityOriginal: qty,
		QuantityOpen:     qty,
		CostOriginal:     moved,
		CostOpen:         moved,
		AcquisitionDate:  lot.AcquisitionDate,
		Sequence:         lot.Sequence,
		Adjustments: []domain.Adjustment{{Kind: string(domain.CADemerger), ExDate: a.ExDate, Ratio: a.RatioString(),
			QtyFrom: domain.Zero, QtyTo: qty, CostFrom: domain.Zero, CostTo: moved,
			Note: fmt.Sprintf("received from %s; acquisition date %s carried over", a.ISIN, lot.AcquisitionDate)}},
	}
	// The resulting company's shares were not listed on 31 Jan 2018, so the ordinary FMV
	// grandfathering does not apply to them; the special proviso is out of scope and flagged.
	res.NewLots = append(res.NewLots, nl)
	res.Notes = append(res.Notes, fmt.Sprintf("DEMERGER on %s: lot %s cost %s -> %s kept, %s moved to new lot %s (%s units of %s)",
		a.ExDate, lot.ID, adj.CostFrom, adj.CostTo, moved, nl.ID, qty, a.NewISIN))
}

// applyMerger: s.47(vii)/s.49(2). The lot closes under the old ISIN and re-opens under the
// new ISIN with the same cost and acquisition date and quantity x swap ratio. The per-unit
// grandfathered FMV is scaled so total FMV is preserved.
func applyMerger(a domain.CorporateAction, factor decimal.Decimal, lot *domain.Lot, res *Result) {
	qty := domain.RoundQty(lot.QuantityOpen.Mul(factor))
	nl := &domain.Lot{
		ID:               deriveID("merger", lot.ID, a.ID, a.NewISIN),
		AccountID:        lot.AccountID,
		ISIN:             a.NewISIN,
		Symbol:           a.NewSymbol,
		OpenTradeID:      lot.ID,
		Origin:           domain.OriginMerger,
		QuantityOriginal: qty,
		QuantityOpen:     qty,
		CostOriginal:     lot.CostOpen,
		CostOpen:         lot.CostOpen,
		AcquisitionDate:  lot.AcquisitionDate,
		Sequence:         lot.Sequence,
		Adjustments:      append(append([]domain.Adjustment{}, lot.Adjustments...), domain.Adjustment{Kind: string(domain.CAMerger), ExDate: a.ExDate, Ratio: a.RatioString(), QtyFrom: lot.QuantityOpen, QtyTo: qty, CostFrom: lot.CostOpen, CostTo: lot.CostOpen, Note: fmt.Sprintf("%s merged into %s; cost and acquisition date carried over", a.ISIN, a.NewISIN)}),
	}
	if lot.GrandfatheredFMV != nil {
		fmv := lot.GrandfatheredFMV.Div(factor)
		nl.GrandfatheredFMV = &fmv
	}
	lot.Adjustments = append(lot.Adjustments, domain.Adjustment{Kind: string(domain.CAMerger), ExDate: a.ExDate, Ratio: a.RatioString(),
		QtyFrom: lot.QuantityOpen, QtyTo: domain.Zero, CostFrom: lot.CostOpen, CostTo: domain.Zero, Note: "moved to " + a.NewISIN})
	lot.QuantityOpen = domain.Zero
	lot.CostOpen = domain.Zero
	res.NewLots = append(res.NewLots, nl)
	res.Notes = append(res.Notes, fmt.Sprintf("MERGER %s on %s: lot %s -> new lot %s (%s units of %s), cost %s carried over",
		a.RatioString(), a.ExDate, lot.ID, nl.ID, qty, a.NewISIN, nl.CostOpen))
}

func deriveID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}
