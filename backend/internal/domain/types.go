// Package domain holds the core types shared by every stage of the engine:
// trades, lots, matches, gains and review items. It has no I/O.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Side of a trade.
type Side string

// Trade sides.
const (
	Buy  Side = "BUY"
	Sell Side = "SELL"
)

// Segment identifies the instrument class.
type Segment string

// Segments. Only EQ, MF (equity-oriented), FUT and OPT are in scope for v1.
const (
	SegEquity     Segment = "EQ"
	SegMutualFund Segment = "MF"
	SegFuture     Segment = "FUT"
	SegOption     Segment = "OPT"
	SegOther      Segment = "OTHER"
)

// IsFNO reports whether the segment is a derivative.
func (s Segment) IsFNO() bool { return s == SegFuture || s == SegOption }

// IsEquityLike reports whether the segment is taxed as STT-paid equity capital gains.
func (s Segment) IsEquityLike() bool { return s == SegEquity || s == SegMutualFund }

// Category is the tax bucket of a gain.
type Category string

// Categories produced by the engine (spec Section 6.2).
const (
	CatSTCGEquity Category = "STCG_EQUITY"
	CatLTCGEquity Category = "LTCG_EQUITY"
	CatIntraday   Category = "INTRADAY"
	CatFNO        Category = "FNO"
	CatBuyback    Category = "BUYBACK"
	CatOutOfScope Category = "OUT_OF_SCOPE"
)

// TradeKind distinguishes ordinary exchange trades from special movements.
type TradeKind string

// Trade kinds.
const (
	KindRegular     TradeKind = "REGULAR"
	KindBuyback     TradeKind = "BUYBACK"      // SELL tendered in a buyback
	KindTransferIn  TradeKind = "TRANSFER_IN"  // off-market receipt into this account
	KindTransferOut TradeKind = "TRANSFER_OUT" // off-market delivery out of this account
	KindOpening     TradeKind = "OPENING"      // user-supplied opening lot (review resolution)
)

// Trade is one normalised tradebook row.
type Trade struct {
	ID         string          `json:"id"` // deterministic hash of the source row
	AccountID  string          `json:"account_id"`
	Broker     string          `json:"broker,omitempty"`
	ISIN       string          `json:"isin"`
	Symbol     string          `json:"symbol"`
	Segment    Segment         `json:"segment"`
	Kind       TradeKind       `json:"kind,omitempty"`
	Side       Side            `json:"side"`
	Quantity   decimal.Decimal `json:"quantity"`
	Price      decimal.Decimal `json:"price"`
	TradeDate  Date            `json:"trade_date"`
	TradeTime  string          `json:"trade_time,omitempty"` // "15:04:05" if known
	Exchange   string          `json:"exchange,omitempty"`
	OrderID    string          `json:"order_id,omitempty"`
	TradeRef   string          `json:"trade_ref,omitempty"`
	SourceFile string          `json:"source_file,omitempty"`
	SourceRow  int             `json:"source_row,omitempty"`
	// Derivatives
	Expiry     Date            `json:"expiry,omitempty"`
	Strike     decimal.Decimal `json:"strike,omitempty"`
	OptionType string          `json:"option_type,omitempty"` // CE / PE
	// Charges actually paid on this trade, if the broker export had them.
	Charges *Charges `json:"charges,omitempty"`
	// For KindTransferIn / KindOpening: the original acquisition facts, if known.
	OriginalAcquisitionDate Date            `json:"original_acquisition_date,omitempty"`
	OriginalCost            decimal.Decimal `json:"original_cost,omitempty"` // total cost for Quantity
	// Raw holds the original columns (never logged).
	Raw map[string]string `json:"-"`
}

// Value is quantity x price, rounded to money scale.
func (t Trade) Value() decimal.Decimal { return RoundMoney(t.Quantity.Mul(t.Price)) }

// ContractKey identifies an F&O contract: underlying + expiry + strike + option type.
func (t Trade) ContractKey() string {
	if !t.Segment.IsFNO() {
		return ""
	}
	key := t.Symbol + "|" + t.Expiry.String()
	if t.Segment == SegOption {
		key += "|" + t.Strike.String() + "|" + t.OptionType
	}
	return key
}

// InstrumentKey is what lots are grouped by: ISIN for cash instruments, the contract key for F&O.
func (t Trade) InstrumentKey() string {
	if t.Segment.IsFNO() {
		return t.ContractKey()
	}
	return t.ISIN
}

// SourceRowHash derives a stable ID from the identifying columns of a source row.
// Re-processing the same file (or the same row in an overlapping export) yields the same ID.
func SourceRowHash(broker, account string, fields ...string) string {
	h := sha256.New()
	h.Write([]byte(broker))
	h.Write([]byte{0})
	h.Write([]byte(account))
	for _, f := range fields {
		h.Write([]byte{0})
		h.Write([]byte(strings.TrimSpace(f)))
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Charges are the transaction costs of one trade.
type Charges struct {
	Brokerage   decimal.Decimal `json:"brokerage"`
	STT         decimal.Decimal `json:"stt"`
	ExchangeFee decimal.Decimal `json:"exchange_fee"`
	SEBIFee     decimal.Decimal `json:"sebi_fee"`
	GST         decimal.Decimal `json:"gst"`
	StampDuty   decimal.Decimal `json:"stamp_duty"`
	Source      string          `json:"source"` // imported | estimated
}

// Total of all charge components.
func (c Charges) Total() decimal.Decimal {
	return c.Brokerage.Add(c.STT).Add(c.ExchangeFee).Add(c.SEBIFee).Add(c.GST).Add(c.StampDuty)
}

// NonSTT is everything except STT (the deductible part for capital gains).
func (c Charges) NonSTT() decimal.Decimal { return c.Total().Sub(c.STT) }

// LotOrigin says how a lot came into existence.
type LotOrigin string

// Lot origins.
const (
	OriginTrade    LotOrigin = "TRADE"
	OriginBonus    LotOrigin = "BONUS"
	OriginDemerger LotOrigin = "DEMERGER"
	OriginMerger   LotOrigin = "MERGER"
	OriginTransfer LotOrigin = "TRANSFER"
	OriginOpening  LotOrigin = "OPENING"
)

// Adjustment records a corporate action applied to a lot.
type Adjustment struct {
	Kind     string          `json:"kind"` // SPLIT, BONUS, DEMERGER, MERGER
	ExDate   Date            `json:"ex_date"`
	Ratio    string          `json:"ratio,omitempty"` // "old:new"
	Note     string          `json:"note,omitempty"`
	QtyFrom  decimal.Decimal `json:"qty_from"`
	QtyTo    decimal.Decimal `json:"qty_to"`
	CostFrom decimal.Decimal `json:"cost_from"`
	CostTo   decimal.Decimal `json:"cost_to"`
}

// Lot is a parcel of units acquired together. Partial sales reduce QuantityOpen and CostOpen.
type Lot struct {
	ID               string          `json:"id"`
	AccountID        string          `json:"account_id"`
	ISIN             string          `json:"isin"`
	Symbol           string          `json:"symbol,omitempty"`
	OpenTradeID      string          `json:"open_trade_id,omitempty"`
	Origin           LotOrigin       `json:"origin"`
	QuantityOriginal decimal.Decimal `json:"quantity_original"`
	QuantityOpen     decimal.Decimal `json:"quantity_open"`
	CostOriginal     decimal.Decimal `json:"cost_original"` // total actual cost incl. buy-side charges
	CostOpen         decimal.Decimal `json:"cost_open"`     // cost still attached to QuantityOpen
	AcquisitionDate  Date            `json:"acquisition_date"`
	// GrandfatheredFMV is the 31 Jan 2018 FMV per unit, already adjusted for later splits.
	// Nil when not applicable (acquired after the cutoff or FMV unknown).
	GrandfatheredFMV *decimal.Decimal `json:"grandfathered_fmv,omitempty"`
	Adjustments      []Adjustment     `json:"adjustments,omitempty"`
	Sequence         int              `json:"-"` // stable tie-break for ordering
}

// CostPerUnit of the open quantity (unrounded; for display).
func (l Lot) CostPerUnit() decimal.Decimal {
	if l.QuantityOpen.IsZero() {
		return Zero
	}
	return l.CostOpen.Div(l.QuantityOpen)
}

// LotMatch is one sale (or part of one) matched against one lot.
type LotMatch struct {
	ID              string          `json:"id"`
	AccountID       string          `json:"account_id"`
	ISIN            string          `json:"isin"`
	Symbol          string          `json:"symbol,omitempty"`
	SellTradeID     string          `json:"sell_trade_id"`
	LotID           string          `json:"lot_id"`
	Quantity        decimal.Decimal `json:"quantity"`
	CostBasis       decimal.Decimal `json:"cost_basis"`   // actual cost of the matched units
	SaleValue       decimal.Decimal `json:"sale_value"`   // gross sale value of the matched units
	SaleCharges     decimal.Decimal `json:"sale_charges"` // non-STT charges apportioned to this match
	AcquisitionDate Date            `json:"acquisition_date"`
	SaleDate        Date            `json:"sale_date"`
	HoldingDays     int             `json:"holding_days"`
	LotOrigin       LotOrigin       `json:"lot_origin"`
	SellKind        TradeKind       `json:"sell_kind,omitempty"`
	// Snapshot of the lot's grandfathering FMV per unit at the time of sale (post-split).
	GrandfatheredFMV *decimal.Decimal `json:"grandfathered_fmv,omitempty"`
	Adjustments      []Adjustment     `json:"adjustments,omitempty"`
}

// IntradayMatch pairs a same-day buy and sell.
type IntradayMatch struct {
	ID          string          `json:"id"`
	AccountID   string          `json:"account_id"`
	ISIN        string          `json:"isin"`
	Symbol      string          `json:"symbol,omitempty"`
	Date        Date            `json:"date"`
	BuyTradeID  string          `json:"buy_trade_id"`
	SellTradeID string          `json:"sell_trade_id"`
	Quantity    decimal.Decimal `json:"quantity"`
	BuyValue    decimal.Decimal `json:"buy_value"`
	SellValue   decimal.Decimal `json:"sell_value"`
	Charges     decimal.Decimal `json:"charges"` // all charges incl. STT (business expense)
}

// FNOMatch is a closed derivative position (opening trade matched FIFO with a closing trade).
type FNOMatch struct {
	ID           string          `json:"id"`
	AccountID    string          `json:"account_id"`
	ContractKey  string          `json:"contract_key"`
	Symbol       string          `json:"symbol"`
	Segment      Segment         `json:"segment"`
	OpenTradeID  string          `json:"open_trade_id"`
	CloseTradeID string          `json:"close_trade_id"`
	OpenSide     Side            `json:"open_side"` // BUY = long, SELL = short
	OpenDate     Date            `json:"open_date"`
	CloseDate    Date            `json:"close_date"`
	Quantity     decimal.Decimal `json:"quantity"`
	OpenValue    decimal.Decimal `json:"open_value"`
	CloseValue   decimal.Decimal `json:"close_value"`
	Charges      decimal.Decimal `json:"charges"`
}

// PnL of the closed position before charges.
func (m FNOMatch) PnL() decimal.Decimal {
	if m.OpenSide == Buy {
		return m.CloseValue.Sub(m.OpenValue)
	}
	return m.OpenValue.Sub(m.CloseValue)
}

// OpenPosition is an F&O position still open at the end of the data.
type OpenPosition struct {
	AccountID   string          `json:"account_id"`
	ContractKey string          `json:"contract_key"`
	Symbol      string          `json:"symbol"`
	Side        Side            `json:"side"`
	Quantity    decimal.Decimal `json:"quantity"`
	OpenDate    Date            `json:"open_date"`
	OpenValue   decimal.Decimal `json:"open_value"`
	TradeID     string          `json:"trade_id"`
}

// ReviewKind classifies review items.
type ReviewKind string

// Review kinds.
const (
	ReviewMissingBuy       ReviewKind = "MISSING_BUY_HISTORY"
	ReviewUnknownISIN      ReviewKind = "UNKNOWN_ISIN"
	ReviewMissingFMV       ReviewKind = "MISSING_FMV_2018"
	ReviewTransferDetails  ReviewKind = "TRANSFER_DETAILS"
	ReviewParseIssue       ReviewKind = "PARSE_ISSUE"
	ReviewOpenPosition     ReviewKind = "OPEN_FNO_POSITION"
	ReviewBuybackOldRegime ReviewKind = "BUYBACK_DEEMED_DIVIDEND"
)

// ReviewItem is something the engine could not decide on its own.
type ReviewItem struct {
	ID        string            `json:"id"`
	Kind      ReviewKind        `json:"kind"`
	AccountID string            `json:"account_id,omitempty"`
	ISIN      string            `json:"isin,omitempty"`
	Symbol    string            `json:"symbol,omitempty"`
	TradeID   string            `json:"trade_id,omitempty"`
	Message   string            `json:"message"`
	Payload   map[string]string `json:"payload,omitempty"`
}

// Gain is one classified gain line for a financial year.
type Gain struct {
	ID              string          `json:"id"`
	FY              string          `json:"fy"`
	Category        Category        `json:"category"`
	AccountID       string          `json:"account_id"`
	ISIN            string          `json:"isin,omitempty"`
	Symbol          string          `json:"symbol,omitempty"`
	ContractKey     string          `json:"contract_key,omitempty"`
	LotMatchID      string          `json:"lot_match_id,omitempty"`
	IntradayMatchID string          `json:"intraday_match_id,omitempty"`
	FNOMatchID      string          `json:"fno_match_id,omitempty"`
	SaleDate        Date            `json:"sale_date"`
	AcquisitionDate Date            `json:"acquisition_date,omitempty"`
	Quantity        decimal.Decimal `json:"quantity"`
	SaleValue       decimal.Decimal `json:"sale_value"`
	ActualCost      decimal.Decimal `json:"actual_cost"`
	// Cost after grandfathering (equals ActualCost when grandfathering does not apply).
	CostOfAcquisition decimal.Decimal `json:"cost_of_acquisition"`
	ChargesDeductible decimal.Decimal `json:"charges_deductible"`
	Gain              decimal.Decimal `json:"gain"` // SaleValue - CostOfAcquisition - ChargesDeductible
	HoldingDays       int             `json:"holding_days,omitempty"`
	RatePeriod        string          `json:"rate_period,omitempty"` // e.g. "2024-07-23..2025-03-31"
	Grandfathering    *Grandfathering `json:"grandfathering,omitempty"`
	BuybackRegime     string          `json:"buyback_regime,omitempty"`
	// BuybackProceeds is the consideration received in a buyback (dividend income under the
	// deemed-dividend regime, exempt under s.115QA); SaleValue is then nil for capital gains.
	BuybackProceeds decimal.Decimal `json:"buyback_proceeds"`
	RulesVersion    string          `json:"rules_version"`
	Steps           []string        `json:"steps,omitempty"` // human-readable derivation
}

// Grandfathering is the comparison made under s.55(2)(ac) 1961 Act / s.90(7) 2025 Act for pre-Feb-2018 lots.
type Grandfathering struct {
	FMVPerUnit  decimal.Decimal `json:"fmv_31_jan_2018_per_unit"`
	FMVTotal    decimal.Decimal `json:"fmv_total"`
	SaleValue   decimal.Decimal `json:"sale_value"`
	ActualCost  decimal.Decimal `json:"actual_cost"`
	ChosenCost  decimal.Decimal `json:"chosen_cost"`
	Applied     bool            `json:"applied"` // true when the chosen cost is not the actual cost
	Explanation string          `json:"explanation"`
}

// CorporateActionKind enumerates supported corporate actions.
type CorporateActionKind string

// Corporate action kinds.
const (
	CASplit    CorporateActionKind = "SPLIT"
	CABonus    CorporateActionKind = "BONUS"
	CADemerger CorporateActionKind = "DEMERGER"
	CAMerger   CorporateActionKind = "MERGER"
)

// CorporateAction is a reference-data event that changes lots on its ex-date.
//
// Ratios are expressed old:new as companies announce them:
//   - SPLIT 1:5 means 1 old share becomes 5 (RatioFrom=1, RatioTo=5).
//   - BONUS 1:1 means 1 new share for every 1 held (RatioFrom=1 held, RatioTo=1 new).
//   - MERGER 10:7 means 10 old shares become 7 new-ISIN shares.
//   - DEMERGER 1:1 means 1 new-ISIN share per 1 old share; CostSplitPct is the share of
//     original cost that moves to the new ISIN under s.49(2C)/(2D).
type CorporateAction struct {
	ID           string              `json:"id"`
	ISIN         string              `json:"isin"`
	Kind         CorporateActionKind `json:"kind"`
	ExDate       Date                `json:"ex_date"`
	RatioFrom    decimal.Decimal     `json:"ratio_from"`
	RatioTo      decimal.Decimal     `json:"ratio_to"`
	CostSplitPct decimal.Decimal     `json:"cost_split_pct,omitempty"` // 0..1, DEMERGER only
	NewISIN      string              `json:"new_isin,omitempty"`
	NewSymbol    string              `json:"new_symbol,omitempty"`
	Source       string              `json:"source,omitempty"`
}

// RatioString formats a corporate action ratio as "from:to".
func (c CorporateAction) RatioString() string {
	return fmt.Sprintf("%s:%s", c.RatioFrom.String(), c.RatioTo.String())
}

// Instrument is reference data for an ISIN.
type Instrument struct {
	ISIN     string           `json:"isin"`
	Symbol   string           `json:"symbol"`
	Name     string           `json:"name,omitempty"`
	Exchange string           `json:"exchange,omitempty"`
	Segment  Segment          `json:"segment"`
	FMV2018  *decimal.Decimal `json:"fmv_2018_01_31,omitempty"`
}
