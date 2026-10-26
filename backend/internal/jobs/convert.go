// Package jobs holds the River workers that run the pure pipeline against the database:
// parse an upload into trades, recompute a user's derived tables, generate reports and
// purge raw files. All I/O lives here; the calculation itself is pipeline.Run.
package jobs

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/store"
)

func toDate(t time.Time) domain.Date { return domain.DateOf(t) }

func toDatePtr(t *time.Time) domain.Date {
	if t == nil {
		return domain.Date{}
	}
	return domain.DateOf(*t)
}

func fromDate(d domain.Date) time.Time { return d.Time() }

func fromDatePtr(d domain.Date) *time.Time {
	if d.IsZero() {
		return nil
	}
	t := d.Time()
	return &t
}

func nullDec(d *decimal.Decimal) decimal.NullDecimal {
	if d == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *d, Valid: true}
}

func decPtr(n decimal.NullDecimal) *decimal.Decimal {
	if !n.Valid {
		return nil
	}
	d := n.Decimal
	return &d
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// TradeFromRow converts a stored trade into the engine's type.
func TradeFromRow(r store.Trade) domain.Trade {
	t := domain.Trade{
		ID: r.SourceRowHash, AccountID: r.AccountID, ISIN: r.Isin, Symbol: r.Symbol, Segment: domain.Segment(r.Segment),
		Kind: domain.TradeKind(r.Kind), Side: domain.Side(r.Side), Quantity: r.Quantity, Price: r.Price, TradeDate: toDate(r.TradeDate),
		TradeTime: r.TradeTime, Exchange: r.Exchange, OrderID: r.OrderID, TradeRef: r.TradeRef, Expiry: toDatePtr(r.Expiry),
		OptionType: r.OptionType, OriginalAcquisitionDate: toDatePtr(r.OriginalAcquisitionDate),
	}
	if r.Strike.Valid {
		t.Strike = r.Strike.Decimal
	}
	if r.OriginalCost.Valid {
		t.OriginalCost = r.OriginalCost.Decimal
	}
	if len(r.Charges) > 0 {
		var c domain.Charges
		if json.Unmarshal(r.Charges, &c) == nil {
			t.Charges = &c
		}
	}
	return t
}

// TradeInsertParams converts an engine trade into an insert.
func TradeInsertParams(userID string, uploadID *string, t domain.Trade) store.InsertTradeParams {
	p := store.InsertTradeParams{
		UserID: userID, AccountID: t.AccountID, UploadID: uploadID, SourceRowHash: t.ID, Isin: t.ISIN, Symbol: t.Symbol,
		Segment: string(t.Segment), Kind: string(t.Kind), Side: string(t.Side), Quantity: t.Quantity, Price: t.Price,
		TradeDate: fromDate(t.TradeDate), TradeTime: t.TradeTime, Exchange: t.Exchange, OrderID: t.OrderID, TradeRef: t.TradeRef,
		Expiry: fromDatePtr(t.Expiry), OptionType: t.OptionType, OriginalAcquisitionDate: fromDatePtr(t.OriginalAcquisitionDate),
	}
	if p.Kind == "" {
		p.Kind = string(domain.KindRegular)
	}
	if !t.Strike.IsZero() {
		p.Strike = decimal.NullDecimal{Decimal: t.Strike, Valid: true}
	}
	if !t.OriginalCost.IsZero() {
		p.OriginalCost = decimal.NullDecimal{Decimal: t.OriginalCost, Valid: true}
	}
	if t.Charges != nil {
		p.Charges, _ = json.Marshal(t.Charges)
	}
	if t.Raw != nil {
		p.Raw, _ = json.Marshal(t.Raw)
	}
	return p
}

// InstrumentFromRow converts reference data.
func InstrumentFromRow(r store.RefInstrument) domain.Instrument {
	return domain.Instrument{ISIN: r.Isin, Symbol: r.Symbol, Name: r.Name, Exchange: r.Exchange, Segment: domain.Segment(r.Segment), FMV2018: decPtr(r.Fmv20180131)}
}

// CorporateActionFromRow converts reference data.
func CorporateActionFromRow(r store.RefCorporateAction) domain.CorporateAction {
	c := domain.CorporateAction{ID: r.ID, ISIN: r.Isin, Kind: domain.CorporateActionKind(r.Kind), ExDate: toDate(r.ExDate),
		RatioFrom: r.RatioFrom, RatioTo: r.RatioTo, NewISIN: deref(r.NewIsin), NewSymbol: deref(r.NewSymbol), Source: r.Source}
	if r.CostSplitPct.Valid {
		c.CostSplitPct = r.CostSplitPct.Decimal
	}
	return c
}

// OpeningLotFromRow converts a user-entered lot.
func OpeningLotFromRow(r store.OpeningLot) domain.Lot {
	return domain.Lot{ID: "opening:" + r.ID, AccountID: r.AccountID, ISIN: r.Isin, Symbol: r.Symbol, Origin: domain.OriginOpening,
		QuantityOriginal: r.Quantity, QuantityOpen: r.Quantity, CostOriginal: r.CostTotal, CostOpen: r.CostTotal, AcquisitionDate: toDate(r.AcquisitionDate)}
}

// GainFromRow rebuilds the engine gain (with explanation) from the stored JSON.
func GainFromRow(explain []byte) (domain.Gain, error) {
	var g domain.Gain
	err := json.Unmarshal(explain, &g)
	return g, err
}
