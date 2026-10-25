// Package matcher is the FIFO engine. It is pure: trades and reference data in, lots,
// matches and review items out. FIFO runs per demat account and per ISIN (CBDT Circular
// 768 of 24 June 1998); lots are never mixed across accounts.
package matcher

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/corpaction"
	"github.com/shreyas0917/taxpl/internal/domain"
)

// Config controls the matching behaviour. Values come from the rules file; the matcher
// itself knows no dates or rates.
type Config struct {
	// IntradayNetting: when true, same-day buys and sells of one ISIN in one account are
	// paired first (up to min(buy, sell)) and only the remainder goes through delivery FIFO.
	IntradayNetting bool
	// GrandfatheringCutoff: lots acquired on or before this date get the 31-Jan-2018 FMV
	// attached when known. Zero disables grandfathering.
	GrandfatheringCutoff domain.Date
	// AsOf: corporate actions with an ex-date up to this date are applied even if no trade
	// follows them. Zero means "up to the last trade".
	AsOf domain.Date
}

// Input to the matcher.
type Input struct {
	Trades           []domain.Trade
	CorporateActions []domain.CorporateAction
	Instruments      map[string]domain.Instrument // by ISIN; supplies FMV and symbol
	OpeningLots      []domain.Lot                 // user-supplied lots (resolved review items)
	Config           Config
}

// Result of matching.
type Result struct {
	Trades        []domain.Trade         `json:"trades"` // canonical order
	Lots          []domain.Lot           `json:"lots"`   // every lot, final state
	Matches       []domain.LotMatch      `json:"matches"`
	Intraday      []domain.IntradayMatch `json:"intraday"`
	FNO           []domain.FNOMatch      `json:"fno"`
	OpenPositions []domain.OpenPosition  `json:"open_positions"`
	Review        []domain.ReviewItem    `json:"review"`
	AuditNotes    []string               `json:"audit_notes"`
}

// Match runs the engine. It never returns an error for data problems: those become review
// items. Errors are reserved for malformed reference data.
func Match(in Input) (Result, error) {
	trades := append([]domain.Trade(nil), in.Trades...)
	SortTrades(trades)
	res := Result{Trades: trades}

	byAccount := map[string][]domain.Trade{}
	var accounts []string
	for _, t := range trades {
		if _, ok := byAccount[t.AccountID]; !ok {
			accounts = append(accounts, t.AccountID)
		}
		byAccount[t.AccountID] = append(byAccount[t.AccountID], t)
	}
	for _, l := range in.OpeningLots {
		if _, ok := byAccount[l.AccountID]; !ok {
			accounts = append(accounts, l.AccountID)
			byAccount[l.AccountID] = nil
		}
	}
	sort.Strings(accounts)

	for _, acct := range accounts {
		var cash, fno []domain.Trade
		for _, t := range byAccount[acct] {
			switch {
			case t.Segment.IsFNO():
				fno = append(fno, t)
			case t.Segment.IsEquityLike():
				cash = append(cash, t)
			default:
				// OUT_OF_SCOPE: surfaced by the classifier, not matched.
			}
		}
		var opening []domain.Lot
		for _, l := range in.OpeningLots {
			if l.AccountID == acct {
				opening = append(opening, l)
			}
		}
		e := newAccountEngine(acct, in, opening)
		if err := e.runCash(cash); err != nil {
			return res, err
		}
		e.runFNO(fno)
		e.collect(&res)
	}
	sortResult(&res)
	return res, nil
}

// SortTrades orders trades canonically: date, time, then content-derived tie-breakers so the
// result does not depend on the order files were supplied in.
func SortTrades(ts []domain.Trade) {
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if c := a.TradeDate.Compare(b.TradeDate); c != 0 {
			return c < 0
		}
		if a.TradeTime != b.TradeTime {
			return a.TradeTime < b.TradeTime
		}
		if a.TradeRef != b.TradeRef {
			return a.TradeRef < b.TradeRef
		}
		return a.ID < b.ID
	})
}

type accountEngine struct {
	acct   string
	in     Input
	lots   map[string][]*domain.Lot // open lot queue per ISIN, FIFO order
	all    []*domain.Lot
	seq    int
	result Result
	fmvAsk map[string]bool
}

func newAccountEngine(acct string, in Input, opening []domain.Lot) *accountEngine {
	e := &accountEngine{acct: acct, in: in, lots: map[string][]*domain.Lot{}, fmvAsk: map[string]bool{}}
	sort.SliceStable(opening, func(i, j int) bool { return opening[i].AcquisitionDate.Before(opening[j].AcquisitionDate) })
	for i := range opening {
		l := opening[i]
		if l.ID == "" {
			l.ID = deriveID("opening", acct, l.ISIN, l.AcquisitionDate.String(), l.QuantityOriginal.String(), l.CostOriginal.String())
		}
		if l.Origin == "" {
			l.Origin = domain.OriginOpening
		}
		if l.QuantityOpen.IsZero() {
			l.QuantityOpen = l.QuantityOriginal
		}
		if l.CostOpen.IsZero() {
			l.CostOpen = l.CostOriginal
		}
		e.addLot(&l)
	}
	return e
}

func (e *accountEngine) nextSeq() int { e.seq++; return e.seq }

func (e *accountEngine) addLot(l *domain.Lot) {
	l.Sequence = e.nextSeq()
	e.attachFMV(l)
	e.all = append(e.all, l)
	e.lots[l.ISIN] = append(e.lots[l.ISIN], l)
}

// attachFMV sets the grandfathering FMV when the lot qualifies and the instrument has one.
func (e *accountEngine) attachFMV(l *domain.Lot) {
	cut := e.in.Config.GrandfatheringCutoff
	if cut.IsZero() || l.AcquisitionDate.After(cut) || l.GrandfatheredFMV != nil {
		return
	}
	if inst, ok := e.in.Instruments[l.ISIN]; ok && inst.FMV2018 != nil {
		fmv := *inst.FMV2018
		l.GrandfatheredFMV = &fmv
		return
	}
	if !e.fmvAsk[l.ISIN] {
		e.fmvAsk[l.ISIN] = true
		e.result.Review = append(e.result.Review, domain.ReviewItem{
			ID: deriveID("review-fmv", e.acct, l.ISIN), Kind: domain.ReviewMissingFMV, AccountID: e.acct, ISIN: l.ISIN, Symbol: l.Symbol,
			Message: fmt.Sprintf("%s was acquired on %s, before 1 Feb 2018, but the 31 Jan 2018 fair market value is not on file. Enter the highest traded price on 31 Jan 2018 so grandfathering can be applied.", l.Symbol, l.AcquisitionDate),
			Payload: map[string]string{"acquisition_date": l.AcquisitionDate.String()},
		})
	}
}

func (e *accountEngine) symbol(isin, fallback string) string {
	if inst, ok := e.in.Instruments[isin]; ok && inst.Symbol != "" {
		return inst.Symbol
	}
	return fallback
}

// runCash processes equity / MF trades day by day with corporate actions applied on ex-date.
func (e *accountEngine) runCash(trades []domain.Trade) error {
	actions := e.relevantActions()
	ai := 0
	applyUpTo := func(d domain.Date) error {
		for ai < len(actions) && !actions[ai].ExDate.After(d) {
			if err := e.applyAction(actions[ai]); err != nil {
				return err
			}
			ai++
		}
		return nil
	}

	for i := 0; i < len(trades); {
		day := trades[i].TradeDate
		// Apply actions with ex-date strictly before today, then those on today (before trades).
		if err := applyUpTo(day); err != nil {
			return err
		}
		j := i
		for j < len(trades) && trades[j].TradeDate.Equal(day) {
			j++
		}
		e.runDay(trades[i:j])
		i = j
	}
	asOf := e.in.Config.AsOf
	if asOf.IsZero() && len(trades) > 0 {
		asOf = trades[len(trades)-1].TradeDate
	}
	if !asOf.IsZero() {
		if err := applyUpTo(asOf); err != nil {
			return err
		}
	}
	return nil
}

// relevantActions returns the corporate actions that can touch this account's lots, in ex-date
// order. Actions chain across ISINs (demerger creates lots in a new ISIN that may itself split),
// so every action is kept and filtered at apply time by whether open lots exist.
func (e *accountEngine) relevantActions() []domain.CorporateAction {
	acts := append([]domain.CorporateAction(nil), e.in.CorporateActions...)
	sort.SliceStable(acts, func(i, j int) bool {
		if c := acts[i].ExDate.Compare(acts[j].ExDate); c != 0 {
			return c < 0
		}
		return acts[i].ID < acts[j].ID
	})
	return acts
}

func (e *accountEngine) applyAction(a domain.CorporateAction) error {
	open := e.lots[a.ISIN]
	if len(open) == 0 {
		return nil
	}
	r, err := corpaction.Apply(a, open)
	if err != nil {
		return err
	}
	for _, note := range r.Notes {
		e.result.AuditNotes = append(e.result.AuditNotes, fmt.Sprintf("[%s] %s", e.acct, note))
	}
	if r.MovedToISIN != "" {
		e.lots[a.ISIN] = nil
	}
	for _, nl := range r.NewLots {
		if nl.Symbol == "" {
			nl.Symbol = e.symbol(nl.ISIN, nl.ISIN)
		}
		e.addLot(nl)
	}
	// Bonus/demerger lots are appended after existing lots of the same ISIN, which keeps FIFO
	// order by acquisition date (a demerger lot inherits an older date but it starts its own
	// ISIN queue, so ordering there is by insertion, which follows the parent lots' order).
	e.sortQueue(a.ISIN)
	for _, nl := range r.NewLots {
		e.sortQueue(nl.ISIN)
	}
	return nil
}

func (e *accountEngine) sortQueue(isin string) {
	q := e.lots[isin]
	sort.SliceStable(q, func(i, j int) bool {
		if c := q[i].AcquisitionDate.Compare(q[j].AcquisitionDate); c != 0 {
			return c < 0
		}
		return q[i].Sequence < q[j].Sequence
	})
	e.lots[isin] = q
}

// runDay handles all trades of one account on one day.
func (e *accountEngine) runDay(day []domain.Trade) {
	byISIN := map[string][]domain.Trade{}
	var isins []string
	for _, t := range day {
		if _, ok := byISIN[t.ISIN]; !ok {
			isins = append(isins, t.ISIN)
		}
		byISIN[t.ISIN] = append(byISIN[t.ISIN], t)
	}
	for _, isin := range isins {
		ts := byISIN[isin]
		remaining := map[string]decimal.Decimal{} // trade ID -> quantity not yet consumed
		for _, t := range ts {
			remaining[t.ID] = t.Quantity
		}
		if e.in.Config.IntradayNetting {
			e.netIntraday(ts, remaining)
		}
		for _, t := range ts {
			qty := remaining[t.ID]
			if !qty.IsPositive() {
				continue
			}
			switch {
			case t.Kind == domain.KindTransferIn || t.Kind == domain.KindOpening:
				e.openTransferLot(t)
			case t.Side == domain.Buy:
				e.openLot(t, qty)
			case t.Side == domain.Sell:
				e.sell(t, qty)
			}
		}
	}
}

// netIntraday pairs same-day regular buys and sells FIFO by time, up to min(buys, sells).
// s.43(5): the squared-off quantity never reaches delivery, so it is speculative income.
func (e *accountEngine) netIntraday(ts []domain.Trade, remaining map[string]decimal.Decimal) {
	var buys, sells []domain.Trade
	totalBuy, totalSell := domain.Zero, domain.Zero
	for _, t := range ts {
		if t.Kind != "" && t.Kind != domain.KindRegular {
			continue
		}
		if t.Side == domain.Buy {
			buys = append(buys, t)
			totalBuy = totalBuy.Add(t.Quantity)
		} else {
			sells = append(sells, t)
			totalSell = totalSell.Add(t.Quantity)
		}
	}
	toNet := domain.Min(totalBuy, totalSell)
	if !toNet.IsPositive() {
		return
	}
	bi := 0
	for _, s := range sells {
		for toNet.IsPositive() && remaining[s.ID].IsPositive() && bi < len(buys) {
			b := buys[bi]
			if !remaining[b.ID].IsPositive() {
				bi++
				continue
			}
			q := domain.Min(domain.Min(remaining[s.ID], remaining[b.ID]), toNet)
			m := domain.IntradayMatch{
				ID: deriveID("intraday", b.ID, s.ID, q.String()), AccountID: e.acct, ISIN: s.ISIN, Symbol: e.symbol(s.ISIN, s.Symbol),
				Date: s.TradeDate, BuyTradeID: b.ID, SellTradeID: s.ID, Quantity: q,
				BuyValue:  domain.ProRata(b.Value(), q, b.Quantity),
				SellValue: domain.ProRata(s.Value(), q, s.Quantity),
				Charges:   chargesShare(b, q).Add(chargesShare(s, q)),
			}
			e.result.Intraday = append(e.result.Intraday, m)
			remaining[s.ID] = remaining[s.ID].Sub(q)
			remaining[b.ID] = remaining[b.ID].Sub(q)
			toNet = toNet.Sub(q)
		}
		if !toNet.IsPositive() {
			break
		}
	}
}

// chargesShare apportions a trade's total charges to a partial quantity.
func chargesShare(t domain.Trade, q decimal.Decimal) decimal.Decimal {
	if t.Charges == nil {
		return domain.Zero
	}
	return domain.ProRata(t.Charges.Total(), q, t.Quantity)
}

// nonSTTShare apportions a trade's non-STT charges to a partial quantity (capital gains).
func nonSTTShare(t domain.Trade, q decimal.Decimal) decimal.Decimal {
	if t.Charges == nil {
		return domain.Zero
	}
	return domain.ProRata(t.Charges.NonSTT(), q, t.Quantity)
}

func (e *accountEngine) openLot(t domain.Trade, qty decimal.Decimal) {
	cost := domain.ProRata(t.Value(), qty, t.Quantity).Add(nonSTTShare(t, qty))
	l := &domain.Lot{
		ID: deriveID("lot", t.ID), AccountID: e.acct, ISIN: t.ISIN, Symbol: e.symbol(t.ISIN, t.Symbol), OpenTradeID: t.ID,
		Origin: domain.OriginTrade, QuantityOriginal: qty, QuantityOpen: qty, CostOriginal: cost, CostOpen: cost, AcquisitionDate: t.TradeDate,
	}
	e.addLot(l)
	e.sortQueue(t.ISIN)
}

// openTransferLot creates a lot for an off-market receipt. Cost and acquisition date carry
// over from the original purchase (no transfer under s.2(47) between the same person's
// accounts); when they are unknown the engine asks instead of guessing.
func (e *accountEngine) openTransferLot(t domain.Trade) {
	if t.OriginalAcquisitionDate.IsZero() {
		e.result.Review = append(e.result.Review, domain.ReviewItem{
			ID: deriveID("review-transfer", t.ID), Kind: domain.ReviewTransferDetails, AccountID: e.acct, ISIN: t.ISIN, Symbol: t.Symbol, TradeID: t.ID,
			Message: fmt.Sprintf("%s units of %s were received into this account on %s by off-market transfer. Enter the original purchase date and total cost so the holding period and gain can be computed.", t.Quantity, t.Symbol, t.TradeDate),
			Payload: map[string]string{"quantity": t.Quantity.String(), "transfer_date": t.TradeDate.String()},
		})
		return
	}
	cost := domain.RoundMoney(t.OriginalCost)
	origin := domain.OriginTransfer
	if t.Kind == domain.KindOpening {
		origin = domain.OriginOpening
	}
	l := &domain.Lot{
		ID: deriveID("lot", t.ID), AccountID: e.acct, ISIN: t.ISIN, Symbol: e.symbol(t.ISIN, t.Symbol), OpenTradeID: t.ID,
		Origin: origin, QuantityOriginal: t.Quantity, QuantityOpen: t.Quantity, CostOriginal: cost, CostOpen: cost, AcquisitionDate: t.OriginalAcquisitionDate,
	}
	e.addLot(l)
	e.sortQueue(t.ISIN)
}

// sell consumes lots FIFO. Whatever cannot be matched becomes a MISSING_BUY_HISTORY review item.
func (e *accountEngine) sell(t domain.Trade, qty decimal.Decimal) {
	q := e.lots[t.ISIN]
	part := 0
	for qty.IsPositive() && len(q) > 0 {
		lot := q[0]
		if !lot.QuantityOpen.IsPositive() {
			q = q[1:]
			continue
		}
		take := domain.Min(qty, lot.QuantityOpen)
		cost := domain.ProRata(lot.CostOpen, take, lot.QuantityOpen)
		part++
		m := domain.LotMatch{
			ID: deriveID("match", t.ID, lot.ID, fmt.Sprint(part)), AccountID: e.acct, ISIN: t.ISIN, Symbol: e.symbol(t.ISIN, t.Symbol),
			SellTradeID: t.ID, LotID: lot.ID, Quantity: take, CostBasis: cost,
			SaleValue:       domain.ProRata(t.Value(), take, t.Quantity),
			SaleCharges:     nonSTTShare(t, take),
			AcquisitionDate: lot.AcquisitionDate, SaleDate: t.TradeDate,
			HoldingDays: lot.AcquisitionDate.DaysUntil(t.TradeDate), LotOrigin: lot.Origin, SellKind: t.Kind,
			Adjustments: append([]domain.Adjustment(nil), lot.Adjustments...),
		}
		if lot.GrandfatheredFMV != nil {
			fmv := *lot.GrandfatheredFMV
			m.GrandfatheredFMV = &fmv
		}
		e.result.Matches = append(e.result.Matches, m)
		lot.QuantityOpen = lot.QuantityOpen.Sub(take)
		lot.CostOpen = lot.CostOpen.Sub(cost)
		qty = qty.Sub(take)
		if lot.QuantityOpen.IsZero() {
			q = q[1:]
		}
	}
	e.lots[t.ISIN] = q
	if qty.IsPositive() {
		e.result.Review = append(e.result.Review, domain.ReviewItem{
			ID: deriveID("review-missing-buy", t.ID), Kind: domain.ReviewMissingBuy, AccountID: e.acct, ISIN: t.ISIN, Symbol: t.Symbol, TradeID: t.ID,
			Message: fmt.Sprintf("We couldn't find the purchase for %s units of %s sold on %s in account %s. Enter the buy date and price, or upload the older tradebook.", qty, t.Symbol, t.TradeDate, e.acct),
			Payload: map[string]string{"quantity": qty.String(), "sale_date": t.TradeDate.String(), "sale_price": t.Price.String()},
		})
	}
}

// --- F&O -------------------------------------------------------------------------------

type openPos struct {
	trade domain.Trade
	qty   decimal.Decimal
}

// runFNO matches derivatives FIFO per contract; a trade of the opposite side closes the oldest
// open position first, and any excess opens a new position (short or long).
func (e *accountEngine) runFNO(trades []domain.Trade) {
	queues := map[string][]openPos{}
	var keys []string
	for _, t := range trades {
		key := t.ContractKey()
		if _, ok := queues[key]; !ok {
			keys = append(keys, key)
		}
		q := queues[key]
		qty := t.Quantity
		part := 0
		for qty.IsPositive() && len(q) > 0 && q[0].trade.Side != t.Side {
			o := &q[0]
			take := domain.Min(qty, o.qty)
			part++
			m := domain.FNOMatch{
				ID: deriveID("fno", o.trade.ID, t.ID, fmt.Sprint(part)), AccountID: e.acct, ContractKey: key, Symbol: t.Symbol, Segment: t.Segment,
				OpenTradeID: o.trade.ID, CloseTradeID: t.ID, OpenSide: o.trade.Side, OpenDate: o.trade.TradeDate, CloseDate: t.TradeDate,
				Quantity:   take,
				OpenValue:  domain.ProRata(o.trade.Value(), take, o.trade.Quantity),
				CloseValue: domain.ProRata(t.Value(), take, t.Quantity),
				Charges:    chargesShare(o.trade, take).Add(chargesShare(t, take)),
			}
			e.result.FNO = append(e.result.FNO, m)
			o.qty = o.qty.Sub(take)
			qty = qty.Sub(take)
			if o.qty.IsZero() {
				q = q[1:]
			}
		}
		if qty.IsPositive() {
			q = append(q, openPos{trade: t, qty: qty})
		}
		queues[key] = q
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, o := range queues[key] {
			e.result.OpenPositions = append(e.result.OpenPositions, domain.OpenPosition{
				AccountID: e.acct, ContractKey: key, Symbol: o.trade.Symbol, Side: o.trade.Side, Quantity: o.qty,
				OpenDate: o.trade.TradeDate, OpenValue: domain.ProRata(o.trade.Value(), o.qty, o.trade.Quantity), TradeID: o.trade.ID,
			})
		}
	}
}

func (e *accountEngine) collect(res *Result) {
	for _, l := range e.all {
		res.Lots = append(res.Lots, *l)
	}
	res.Matches = append(res.Matches, e.result.Matches...)
	res.Intraday = append(res.Intraday, e.result.Intraday...)
	res.FNO = append(res.FNO, e.result.FNO...)
	res.OpenPositions = append(res.OpenPositions, e.result.OpenPositions...)
	res.Review = append(res.Review, e.result.Review...)
	res.AuditNotes = append(res.AuditNotes, e.result.AuditNotes...)
}

func sortResult(res *Result) {
	sort.SliceStable(res.Lots, func(i, j int) bool {
		a, b := res.Lots[i], res.Lots[j]
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.ISIN != b.ISIN {
			return a.ISIN < b.ISIN
		}
		if c := a.AcquisitionDate.Compare(b.AcquisitionDate); c != 0 {
			return c < 0
		}
		return a.ID < b.ID
	})
	sort.SliceStable(res.Matches, func(i, j int) bool {
		a, b := res.Matches[i], res.Matches[j]
		if c := a.SaleDate.Compare(b.SaleDate); c != 0 {
			return c < 0
		}
		if a.AccountID != b.AccountID {
			return a.AccountID < b.AccountID
		}
		if a.ISIN != b.ISIN {
			return a.ISIN < b.ISIN
		}
		if a.SellTradeID != b.SellTradeID {
			return a.SellTradeID < b.SellTradeID
		}
		if c := a.AcquisitionDate.Compare(b.AcquisitionDate); c != 0 {
			return c < 0
		}
		return a.ID < b.ID
	})
	sort.SliceStable(res.Intraday, func(i, j int) bool {
		a, b := res.Intraday[i], res.Intraday[j]
		if c := a.Date.Compare(b.Date); c != 0 {
			return c < 0
		}
		return a.ID < b.ID
	})
	sort.SliceStable(res.FNO, func(i, j int) bool {
		a, b := res.FNO[i], res.FNO[j]
		if c := a.CloseDate.Compare(b.CloseDate); c != 0 {
			return c < 0
		}
		return a.ID < b.ID
	})
	sort.SliceStable(res.Review, func(i, j int) bool { return res.Review[i].ID < res.Review[j].ID })
}

func deriveID(parts ...string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h.Sum(nil))[:24]
}
