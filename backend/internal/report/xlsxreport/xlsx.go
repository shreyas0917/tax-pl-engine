// Package xlsxreport renders a pipeline report as an Excel workbook with excelize.
// Sheets: Summary, Gains, Intraday, F&O, Holdings, Review, Trades. Every number is written
// as a numeric cell from its decimal string so Excel shows exact values with INR formatting.
package xlsxreport

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/tax"
)

// ContentType of the output.
const ContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

type writer struct {
	f     *excelize.File
	money int
	qty   int
	head  int
	title int
	note  int
	gain  int
	loss  int
}

// Write renders the report for one financial year (fy "" = every year in the report).
func Write(rep *pipeline.Report, fy string) ([]byte, string, error) {
	f := excelize.NewFile()
	w := &writer{f: f}
	var err error
	if w.money, err = f.NewStyle(&excelize.Style{NumFmt: 4}); err != nil { // #,##0.00
		return nil, "", err
	}
	if w.qty, err = f.NewStyle(&excelize.Style{CustomNumFmt: strPtr("#,##0.######")}); err != nil {
		return nil, "", err
	}
	if w.head, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E8EEF2"}}, Border: []excelize.Border{{Type: "bottom", Color: "#9AA5AD", Style: 1}}}); err != nil {
		return nil, "", err
	}
	if w.title, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}}); err != nil {
		return nil, "", err
	}
	if w.note, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Italic: true, Color: "#55606A"}}); err != nil {
		return nil, "", err
	}
	if w.gain, err = f.NewStyle(&excelize.Style{NumFmt: 4, Font: &excelize.Font{Color: "#1F7A4D"}}); err != nil {
		return nil, "", err
	}
	if w.loss, err = f.NewStyle(&excelize.Style{NumFmt: 4, Font: &excelize.Font{Color: "#B3261E"}}); err != nil {
		return nil, "", err
	}

	dataChecksum := ""
	if b, err := jsonreport.Marshal(rep); err == nil {
		dataChecksum = jsonreport.Checksum(b)
	}
	if err := w.summary(rep, fy, dataChecksum); err != nil {
		return nil, "", err
	}
	if err := w.gains(rep, fy); err != nil {
		return nil, "", err
	}
	if err := w.intraday(rep, fy); err != nil {
		return nil, "", err
	}
	if err := w.fno(rep, fy); err != nil {
		return nil, "", err
	}
	if err := w.holdings(rep); err != nil {
		return nil, "", err
	}
	if err := w.review(rep); err != nil {
		return nil, "", err
	}
	if err := w.trades(rep, fy); err != nil {
		return nil, "", err
	}
	f.SetActiveSheet(0)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), ContentType, nil
}

func strPtr(s string) *string { return &s }

func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

func (w *writer) setRow(sheet string, row int, values ...any) {
	for i, v := range values {
		_ = w.f.SetCellValue(sheet, cell(i+1, row), v)
	}
}

func (w *writer) setHeader(sheet string, row int, names ...string) {
	for i, n := range names {
		_ = w.f.SetCellValue(sheet, cell(i+1, row), n)
	}
	_ = w.f.SetCellStyle(sheet, cell(1, row), cell(len(names), row), w.head)
}

// num writes a decimal as a numeric cell with money or quantity formatting.
func (w *writer) num(sheet string, col, row int, d decimal.Decimal, style int) {
	v, _ := d.Float64() // nofloat:allow — display only; the source of truth stays decimal
	_ = w.f.SetCellValue(sheet, cell(col, row), v)
	_ = w.f.SetCellStyle(sheet, cell(col, row), cell(col, row), style)
}

func (w *writer) signed(sheet string, col, row int, d decimal.Decimal) {
	style := w.gain
	if d.IsNegative() {
		style = w.loss
	}
	w.num(sheet, col, row, d, style)
}

func inYear(fy string, s string) bool { return fy == "" || fy == s }

func (w *writer) summary(rep *pipeline.Report, fy, checksum string) error {
	const sheet = "Summary"
	if err := w.f.SetSheetName("Sheet1", sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "A", 34)
	_ = w.f.SetColWidth(sheet, "B", "G", 18)
	row := 1
	_ = w.f.SetCellValue(sheet, cell(1, row), "Tax P&L Engine report")
	_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.title)
	row++
	_ = w.f.SetCellValue(sheet, cell(1, row), pipeline.Disclaimer)
	_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
	row++
	_ = w.f.SetCellValue(sheet, cell(1, row), "Data fingerprint (SHA-256 of the computation): "+checksum)
	_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
	row += 2

	for _, s := range rep.Summaries {
		if !inYear(fy, s.FY) {
			continue
		}
		_ = w.f.SetCellValue(sheet, cell(1, row), fmt.Sprintf("%s  |  %s  |  rules %s", s.YearLabel, s.Law, s.RulesVersion))
		_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.title)
		row++
		verified := "Rules not yet signed off by a CA"
		if s.VerifiedBy != "" {
			verified = "Rules verified by " + s.VerifiedBy
		}
		_ = w.f.SetCellValue(sheet, cell(1, row), verified)
		_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
		row += 2

		w.setHeader(sheet, row, "Category", "Lines", "Sale value", "Cost", "Charges", "Gain / loss")
		row++
		for _, c := range s.Categories {
			w.setRow(sheet, row, string(c.Category), c.Count)
			w.num(sheet, 3, row, c.SaleValue, w.money)
			w.num(sheet, 4, row, c.Cost, w.money)
			w.num(sheet, 5, row, c.Charges, w.money)
			w.signed(sheet, 6, row, c.Gain)
			row++
		}
		row++
		w.setHeader(sheet, row, "Total", "Amount")
		row++
		for _, kv := range []struct {
			k string
			v decimal.Decimal
		}{{"Short-term capital gains (" + s.References["stcg"] + ")", s.STCG}, {"Long-term capital gains (" + s.References["ltcg"] + ")", s.LTCG},
			{"Intraday / speculative business income", s.Intraday}, {"F&O / non-speculative business income", s.FNO}, {"F&O turnover (ICAI: sum of absolute P&L)", s.FNOTurnover},
			{"Buyback proceeds taxed as deemed dividend", s.BuybackDividend}, {"Buyback proceeds exempt (s.115QA era)", s.BuybackExempt}} {
			_ = w.f.SetCellValue(sheet, cell(1, row), kv.k)
			w.signed(sheet, 2, row, kv.v)
			row++
		}
		row++
		if len(s.Periods) > 1 {
			w.setHeader(sheet, row, "Rate period", "STCG", "STCG rate", "LTCG", "LTCG rate", "Exemption allocated", "LTCG taxable")
			row++
			for _, p := range s.Periods {
				w.setRow(sheet, row, p.Period)
				w.signed(sheet, 2, row, p.STCG)
				_ = w.f.SetCellValue(sheet, cell(3, row), pct(p.STCGRate))
				w.signed(sheet, 4, row, p.LTCG)
				_ = w.f.SetCellValue(sheet, cell(5, row), pct(p.LTCGRate))
				w.num(sheet, 6, row, p.LTCGExemptionAllocated, w.money)
				w.num(sheet, 7, row, p.LTCGTaxable, w.money)
				row++
			}
			row++
		}
		w.setHeader(sheet, row, "Estimate (not advice)", "Amount")
		row++
		_ = w.f.SetCellValue(sheet, cell(1, row), "LTCG exemption for the year")
		w.num(sheet, 2, row, s.LTCGExemption, w.money)
		row++
		_ = w.f.SetCellValue(sheet, cell(1, row), "LTCG taxable after exemption")
		w.num(sheet, 2, row, s.LTCGTaxableAfterExempt, w.money)
		row++
		_ = w.f.SetCellValue(sheet, cell(1, row), "Estimated tax on equity gains")
		w.num(sheet, 2, row, s.EstimatedTax, w.money)
		row++
		for _, n := range s.EstimateNotes {
			_ = w.f.SetCellValue(sheet, cell(1, row), n)
			_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
			row++
		}
		_ = w.f.SetCellValue(sheet, cell(1, row), s.AuditNote)
		_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
		row += 2
		if len(s.Losses) > 0 {
			w.setHeader(sheet, row, "Loss", "Amount", "Set off against", "Carry forward years", "Carry forward against")
			row++
			for _, l := range s.Losses {
				w.setRow(sheet, row, l.Loss)
				w.num(sheet, 2, row, l.Amount, w.money)
				_ = w.f.SetCellValue(sheet, cell(3, row), strings.Join(l.SetOffAgainst, ", "))
				_ = w.f.SetCellValue(sheet, cell(4, row), l.CarryForwardYears)
				_ = w.f.SetCellValue(sheet, cell(5, row), strings.Join(l.CarryForwardAgainst, ", "))
				row++
			}
			_ = w.f.SetCellValue(sheet, cell(1, row), "Losses can be carried forward only if the return is filed by the due date.")
			_ = w.f.SetCellStyle(sheet, cell(1, row), cell(1, row), w.note)
			row += 2
		}
	}
	return nil
}

func pct(d decimal.Decimal) string { return d.Mul(decimal.NewFromInt(100)).String() + "%" }

func (w *writer) gains(rep *pipeline.Report, fy string) error {
	const sheet = "Gains"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "C", 14)
	_ = w.f.SetColWidth(sheet, "D", "D", 22)
	_ = w.f.SetColWidth(sheet, "E", "O", 15)
	w.setHeader(sheet, 1, "FY", "Category", "Account", "Symbol", "ISIN", "Sale date", "Acquired", "Holding days", "Quantity", "Sale value", "Actual cost", "Cost of acquisition", "Charges", "Gain / loss", "Grandfathered", "Rate period", "Rules")
	row := 2
	for _, g := range rep.Gains {
		if !inYear(fy, g.FY) || g.Category == domain.CatIntraday || g.Category == domain.CatFNO {
			continue
		}
		gf := ""
		if g.Grandfathering != nil {
			gf = map[bool]string{true: "FMV used", false: "actual cost"}[g.Grandfathering.Applied]
		}
		w.setRow(sheet, row, g.FY, string(g.Category), g.AccountID, g.Symbol, g.ISIN, g.SaleDate.String(), g.AcquisitionDate.String(), g.HoldingDays)
		w.num(sheet, 9, row, g.Quantity, w.qty)
		w.num(sheet, 10, row, g.SaleValue, w.money)
		w.num(sheet, 11, row, g.ActualCost, w.money)
		w.num(sheet, 12, row, g.CostOfAcquisition, w.money)
		w.num(sheet, 13, row, g.ChargesDeductible, w.money)
		w.signed(sheet, 14, row, g.Gain)
		_ = w.f.SetCellValue(sheet, cell(15, row), gf)
		_ = w.f.SetCellValue(sheet, cell(16, row), g.RatePeriod)
		_ = w.f.SetCellValue(sheet, cell(17, row), g.RulesVersion)
		row++
	}
	_ = w.f.AutoFilter(sheet, "A1:Q1", nil)
	return w.freeze(sheet)
}

func (w *writer) intraday(rep *pipeline.Report, fy string) error {
	const sheet = "Intraday"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "H", 16)
	w.setHeader(sheet, 1, "FY", "Account", "Symbol", "Date", "Quantity", "Buy value", "Sell value", "Charges", "Profit / loss")
	row := 2
	for _, g := range rep.Gains {
		if !inYear(fy, g.FY) || g.Category != domain.CatIntraday {
			continue
		}
		w.setRow(sheet, row, g.FY, g.AccountID, g.Symbol, g.SaleDate.String())
		w.num(sheet, 5, row, g.Quantity, w.qty)
		w.num(sheet, 6, row, g.CostOfAcquisition, w.money)
		w.num(sheet, 7, row, g.SaleValue, w.money)
		w.num(sheet, 8, row, g.ChargesDeductible, w.money)
		w.signed(sheet, 9, row, g.Gain)
		row++
	}
	return w.freeze(sheet)
}

func (w *writer) fno(rep *pipeline.Report, fy string) error {
	const sheet = "F&O"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "A", 10)
	_ = w.f.SetColWidth(sheet, "B", "C", 18)
	_ = w.f.SetColWidth(sheet, "D", "D", 34)
	_ = w.f.SetColWidth(sheet, "E", "K", 15)
	w.setHeader(sheet, 1, "FY", "Account", "Underlying", "Contract", "Opened", "Closed", "Quantity", "Open value", "Close value", "Charges", "Profit / loss", "|P&L| (turnover)")
	row := 2
	for _, g := range rep.Gains {
		if !inYear(fy, g.FY) || g.Category != domain.CatFNO {
			continue
		}
		w.setRow(sheet, row, g.FY, g.AccountID, g.Symbol, g.ContractKey, g.AcquisitionDate.String(), g.SaleDate.String())
		w.num(sheet, 7, row, g.Quantity, w.qty)
		w.num(sheet, 8, row, g.ActualCost, w.money)
		w.num(sheet, 9, row, g.SaleValue, w.money)
		w.num(sheet, 10, row, g.ChargesDeductible, w.money)
		w.signed(sheet, 11, row, g.Gain)
		w.num(sheet, 12, row, g.SaleValue.Sub(g.ActualCost).Abs(), w.money)
		row++
	}
	if len(rep.OpenPositions) > 0 {
		row++
		w.setHeader(sheet, row, "Open positions (no closing trade yet)", "Account", "Contract", "Side", "Quantity", "Opened", "Open value")
		row++
		for _, p := range rep.OpenPositions {
			w.setRow(sheet, row, "", p.AccountID, p.ContractKey, string(p.Side))
			w.num(sheet, 5, row, p.Quantity, w.qty)
			_ = w.f.SetCellValue(sheet, cell(6, row), p.OpenDate.String())
			w.num(sheet, 7, row, p.OpenValue, w.money)
			row++
		}
	}
	return w.freeze(sheet)
}

func (w *writer) holdings(rep *pipeline.Report) error {
	const sheet = "Holdings"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "I", 16)
	w.setHeader(sheet, 1, "Account", "Symbol", "ISIN", "Origin", "Acquired", "Quantity open", "Cost open", "Cost / unit", "FMV 31-Jan-2018 / unit")
	row := 2
	for _, l := range rep.Lots {
		if !l.QuantityOpen.IsPositive() {
			continue
		}
		w.setRow(sheet, row, l.AccountID, l.Symbol, l.ISIN, string(l.Origin), l.AcquisitionDate.String())
		w.num(sheet, 6, row, l.QuantityOpen, w.qty)
		w.num(sheet, 7, row, l.CostOpen, w.money)
		w.num(sheet, 8, row, l.CostPerUnit().Round(4), w.money)
		if l.GrandfatheredFMV != nil {
			w.num(sheet, 9, row, *l.GrandfatheredFMV, w.money)
		}
		row++
	}
	return w.freeze(sheet)
}

func (w *writer) review(rep *pipeline.Report) error {
	const sheet = "Review"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "A", 26)
	_ = w.f.SetColWidth(sheet, "B", "C", 16)
	_ = w.f.SetColWidth(sheet, "D", "D", 110)
	w.setHeader(sheet, 1, "Kind", "Account", "Symbol", "What we need from you")
	row := 2
	for _, r := range rep.Review {
		w.setRow(sheet, row, string(r.Kind), r.AccountID, r.Symbol, r.Message)
		row++
	}
	if row == 2 {
		_ = w.f.SetCellValue(sheet, "A2", "Nothing needs review.")
	}
	return nil
}

func (w *writer) trades(rep *pipeline.Report, fy string) error {
	const sheet = "Trades"
	if _, err := w.f.NewSheet(sheet); err != nil {
		return err
	}
	_ = w.f.SetColWidth(sheet, "A", "L", 15)
	w.setHeader(sheet, 1, "Date", "Time", "Account", "Symbol", "ISIN", "Segment", "Kind", "Side", "Quantity", "Price", "Value", "Exchange", "Source file", "Row")
	row := 2
	for _, t := range rep.Trades {
		if !inYear(fy, domain.FinancialYear(t.TradeDate)) {
			continue
		}
		w.setRow(sheet, row, t.TradeDate.String(), t.TradeTime, t.AccountID, t.Symbol, t.ISIN, string(t.Segment), string(t.Kind), string(t.Side))
		w.num(sheet, 9, row, t.Quantity, w.qty)
		w.num(sheet, 10, row, t.Price, w.money)
		w.num(sheet, 11, row, t.Value(), w.money)
		_ = w.f.SetCellValue(sheet, cell(12, row), t.Exchange)
		_ = w.f.SetCellValue(sheet, cell(13, row), t.SourceFile)
		_ = w.f.SetCellValue(sheet, cell(14, row), t.SourceRow)
		row++
	}
	return w.freeze(sheet)
}

func (w *writer) freeze(sheet string) error {
	return w.f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
}

// Ensure the tax package stays referenced for the summary types used above.
var _ tax.Summary
