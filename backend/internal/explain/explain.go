// Package explain builds the "why this number?" tree for a gain line from a pipeline report.
// It is pure: it only reads the report and the gain.
package explain

import (
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/pipeline"
)

// LotView is one matched lot as shown in the explain drawer.
type LotView struct {
	LotID          string                 `json:"lot_id"`
	Account        string                 `json:"account"`
	Origin         domain.LotOrigin       `json:"origin"`
	Acquired       domain.Date            `json:"acquired"`
	Quantity       decimal.Decimal        `json:"quantity"`
	ActualCost     decimal.Decimal        `json:"actual_cost"`
	CostPerUnit    decimal.Decimal        `json:"cost_per_unit"`
	HoldingDays    int                    `json:"holding_days"`
	Grandfathering *domain.Grandfathering `json:"grandfathering,omitempty"`
	Adjustments    []domain.Adjustment    `json:"adjustments"`
	OpenTrade      *TradeView             `json:"open_trade,omitempty"`
}

// TradeView is a trade as shown in the drawer.
type TradeView struct {
	ID        string           `json:"id"`
	Date      domain.Date      `json:"date"`
	Time      string           `json:"time,omitempty"`
	Side      domain.Side      `json:"side"`
	Kind      domain.TradeKind `json:"kind"`
	Quantity  decimal.Decimal  `json:"quantity"`
	Price     decimal.Decimal  `json:"price"`
	Value     decimal.Decimal  `json:"value"`
	Exchange  string           `json:"exchange,omitempty"`
	Source    string           `json:"source,omitempty"`
	SourceRow int              `json:"source_row,omitempty"`
	Charges   *domain.Charges  `json:"charges,omitempty"`
}

// Tree is the full explanation.
type Tree struct {
	GainID            string          `json:"gain_id"`
	Category          domain.Category `json:"category"`
	FY                string          `json:"fy"`
	Account           string          `json:"account"`
	Symbol            string          `json:"symbol"`
	ISIN              string          `json:"isin,omitempty"`
	ContractKey       string          `json:"contract_key,omitempty"`
	Gain              decimal.Decimal `json:"gain"`
	SaleValue         decimal.Decimal `json:"sale_value"`
	CostOfAcquisition decimal.Decimal `json:"cost_of_acquisition"`
	ChargesDeductible decimal.Decimal `json:"charges_deductible"`
	Sale              *TradeView      `json:"sale,omitempty"`
	Buy               *TradeView      `json:"buy,omitempty"` // intraday / F&O opening trade
	Lots              []LotView       `json:"lots"`
	RulesVersion      string          `json:"rules_version"`
	RatePeriod        string          `json:"rate_period,omitempty"`
	BuybackRegime     string          `json:"buyback_regime,omitempty"`
	Steps             []string        `json:"steps"`
	Disclaimer        string          `json:"disclaimer"`
}

func tradeView(t domain.Trade) *TradeView {
	return &TradeView{ID: t.ID, Date: t.TradeDate, Time: t.TradeTime, Side: t.Side, Kind: t.Kind, Quantity: t.Quantity, Price: t.Price, Value: t.Value(),
		Exchange: t.Exchange, Source: t.SourceFile, SourceRow: t.SourceRow, Charges: t.Charges}
}

// Build locates the gain in the report and assembles its tree. accountName replaces the
// account ID for display when non-empty.
func Build(rep *pipeline.Report, g domain.Gain, accountName string) (Tree, error) {
	acct := g.AccountID
	if accountName != "" {
		acct = accountName
	}
	tree := Tree{GainID: g.ID, Category: g.Category, FY: g.FY, Account: acct, Symbol: g.Symbol, ISIN: g.ISIN, ContractKey: g.ContractKey, Gain: g.Gain,
		SaleValue: g.SaleValue, CostOfAcquisition: g.CostOfAcquisition, ChargesDeductible: g.ChargesDeductible, RulesVersion: g.RulesVersion,
		RatePeriod: g.RatePeriod, BuybackRegime: g.BuybackRegime, Steps: g.Steps, Disclaimer: pipeline.Disclaimer, Lots: []LotView{}}
	if tree.Steps == nil {
		tree.Steps = []string{}
	}
	trades := map[string]domain.Trade{}
	for _, t := range rep.Trades {
		trades[t.ID] = t
	}
	lots := map[string]domain.Lot{}
	for _, l := range rep.Lots {
		lots[l.ID] = l
	}
	lookup := func(id string) *TradeView {
		if t, ok := trades[id]; ok {
			return tradeView(t)
		}
		return nil
	}
	switch {
	case g.LotMatchID != "":
		for _, m := range rep.Matches {
			if m.ID != g.LotMatchID {
				continue
			}
			tree.Sale = lookup(m.SellTradeID)
			lot := lots[m.LotID]
			lv := LotView{LotID: m.LotID, Account: acct, Origin: m.LotOrigin, Acquired: m.AcquisitionDate, Quantity: m.Quantity, ActualCost: m.CostBasis,
				HoldingDays: m.HoldingDays, Grandfathering: g.Grandfathering, Adjustments: m.Adjustments, OpenTrade: lookup(lot.OpenTradeID)}
			if m.Quantity.IsPositive() {
				lv.CostPerUnit = m.CostBasis.Div(m.Quantity).Round(4)
			}
			if lv.Adjustments == nil {
				lv.Adjustments = []domain.Adjustment{}
			}
			tree.Lots = append(tree.Lots, lv)
			return tree, nil
		}
		return tree, fmt.Errorf("lot match %s not found in report", g.LotMatchID)
	case g.IntradayMatchID != "":
		for _, m := range rep.Intraday {
			if m.ID == g.IntradayMatchID {
				tree.Sale, tree.Buy = lookup(m.SellTradeID), lookup(m.BuyTradeID)
				return tree, nil
			}
		}
		return tree, fmt.Errorf("intraday match %s not found", g.IntradayMatchID)
	case g.FNOMatchID != "":
		for _, m := range rep.FNO {
			if m.ID == g.FNOMatchID {
				tree.Sale, tree.Buy = lookup(m.CloseTradeID), lookup(m.OpenTradeID)
				return tree, nil
			}
		}
		return tree, fmt.Errorf("F&O match %s not found", g.FNOMatchID)
	}
	return tree, nil
}
