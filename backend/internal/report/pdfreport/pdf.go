// Package pdfreport renders a one-year summary plus gain tables as a PDF with go-pdf/fpdf.
package pdfreport

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/tax"
)

// ContentType of the output.
const ContentType = "application/pdf"

type doc struct {
	pdf *fpdf.Fpdf
}

// Write renders the report. fy "" renders every year in the report.
func Write(rep *pipeline.Report, fy string) ([]byte, string, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(12, 14, 12)
	pdf.SetAutoPageBreak(true, 16)
	d := &doc{pdf: pdf}
	checksum := ""
	if b, err := jsonreport.Marshal(rep); err == nil {
		checksum = jsonreport.Checksum(b)
	}
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "I", 7)
		pdf.SetTextColor(90, 90, 90)
		pdf.CellFormat(0, 5, pipeline.Disclaimer+"  |  Data fingerprint "+checksum[:16]+"  |  Page "+fmt.Sprint(pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	for _, s := range rep.Summaries {
		if fy != "" && s.FY != fy {
			continue
		}
		d.summaryPage(s)
		d.gainsTable(rep, s.FY)
	}
	if pdf.PageNo() == 0 {
		pdf.AddPage()
		pdf.SetFont("Helvetica", "", 11)
		pdf.Cell(0, 8, "No gains were computed for the requested year.")
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), ContentType, nil
}

func (d *doc) h1(s string) {
	d.pdf.SetFont("Helvetica", "B", 15)
	d.pdf.SetTextColor(20, 20, 20)
	d.pdf.Cell(0, 9, s)
	d.pdf.Ln(9)
}

func (d *doc) h2(s string) {
	d.pdf.Ln(2)
	d.pdf.SetFont("Helvetica", "B", 11)
	d.pdf.SetTextColor(20, 20, 20)
	d.pdf.Cell(0, 7, s)
	d.pdf.Ln(7)
}

func (d *doc) note(s string) {
	d.pdf.SetFont("Helvetica", "I", 8)
	d.pdf.SetTextColor(90, 90, 90)
	d.pdf.MultiCell(0, 4, s, "", "L", false)
}

func (d *doc) kv(k string, v decimal.Decimal) {
	d.pdf.SetFont("Helvetica", "", 9.5)
	d.pdf.SetTextColor(20, 20, 20)
	d.pdf.CellFormat(120, 6, k, "", 0, "L", false, 0, "")
	if v.IsNegative() {
		d.pdf.SetTextColor(179, 38, 30)
	} else {
		d.pdf.SetTextColor(31, 122, 77)
	}
	d.pdf.CellFormat(40, 6, inr(v), "", 1, "R", false, 0, "")
	d.pdf.SetTextColor(20, 20, 20)
}

func (d *doc) summaryPage(s tax.Summary) {
	d.pdf.AddPage()
	d.h1("Tax P&L Engine report: " + s.YearLabel)
	d.note(fmt.Sprintf("%s  |  rules %s  |  %s", s.Law, s.RulesVersion, map[bool]string{true: "verified by " + s.VerifiedBy, false: "rules not yet signed off by a CA"}[s.VerifiedBy != ""]))
	d.h2("Totals")
	d.kv("Short-term capital gains ("+s.References["stcg"]+")", s.STCG)
	d.kv("Long-term capital gains ("+s.References["ltcg"]+")", s.LTCG)
	d.kv("Intraday (speculative business income)", s.Intraday)
	d.kv("F&O (non-speculative business income)", s.FNO)
	d.kv("F&O turnover (sum of absolute P&L)", s.FNOTurnover)
	if !s.BuybackDividend.IsZero() {
		d.kv("Buyback proceeds taxed as deemed dividend", s.BuybackDividend)
	}
	if !s.BuybackExempt.IsZero() {
		d.kv("Buyback proceeds exempt (s.115QA era)", s.BuybackExempt)
	}
	d.h2("By category")
	d.tableHeader([]string{"Category", "Lines", "Sale value", "Cost", "Charges", "Gain / loss"}, []float64{60, 20, 40, 40, 35, 40})
	for _, c := range s.Categories {
		d.tableRow([]string{string(c.Category), fmt.Sprint(c.Count), inr(c.SaleValue), inr(c.Cost), inr(c.Charges), inr(c.Gain)}, []float64{60, 20, 40, 40, 35, 40}, c.Gain)
	}
	if len(s.Periods) > 1 {
		d.h2("Rate periods (the rates changed during the year)")
		d.tableHeader([]string{"Period", "STCG", "Rate", "LTCG", "Rate", "Exemption allocated", "LTCG taxable"}, []float64{60, 35, 18, 35, 18, 40, 35})
		for _, p := range s.Periods {
			d.tableRow([]string{p.Period, inr(p.STCG), pct(p.STCGRate), inr(p.LTCG), pct(p.LTCGRate), inr(p.LTCGExemptionAllocated), inr(p.LTCGTaxable)}, []float64{60, 35, 18, 35, 18, 40, 35}, decimal.Zero)
		}
	}
	d.h2("Estimate (not advice)")
	d.kv("LTCG exemption for the year", s.LTCGExemption)
	d.kv("LTCG taxable after exemption", s.LTCGTaxableAfterExempt)
	d.kv("Estimated tax on equity gains", s.EstimatedTax)
	for _, n := range s.EstimateNotes {
		d.note(n)
	}
	d.note(s.AuditNote)
	if len(s.Losses) > 0 {
		d.h2("Losses")
		d.tableHeader([]string{"Loss", "Amount", "Set off against", "Carry forward", "Against"}, []float64{55, 35, 60, 25, 60})
		for _, l := range s.Losses {
			d.tableRow([]string{l.Loss, inr(l.Amount), strings.Join(l.SetOffAgainst, ", "), fmt.Sprintf("%d years", l.CarryForwardYears), strings.Join(l.CarryForwardAgainst, ", ")}, []float64{55, 35, 60, 25, 60}, decimal.Zero)
		}
		d.note("Losses can be carried forward only if the return is filed by the due date.")
	}
}

func (d *doc) gainsTable(rep *pipeline.Report, fy string) {
	widths := []float64{22, 26, 30, 22, 22, 16, 22, 30, 30, 22, 30, 18}
	header := []string{"Category", "Account", "Symbol", "Sold", "Acquired", "Qty", "Days", "Sale value", "Cost", "Charges", "Gain / loss", "GF"}
	first := true
	for _, g := range rep.Gains {
		if g.FY != fy {
			continue
		}
		if first {
			d.pdf.AddPage()
			d.h1("Gain lines: " + fy)
			d.note("GF = grandfathering: FMV means the 31-Jan-2018 fair market value was used as cost; actual means the purchase cost was higher.")
			d.tableHeader(header, widths)
			first = false
		}
		if d.pdf.GetY() > 185 {
			d.pdf.AddPage()
			d.tableHeader(header, widths)
		}
		gf := ""
		if g.Grandfathering != nil {
			gf = map[bool]string{true: "FMV", false: "actual"}[g.Grandfathering.Applied]
		}
		acq := g.AcquisitionDate.String()
		if g.Category == domain.CatIntraday {
			acq = "same day"
		}
		d.tableRow([]string{string(g.Category), trunc(g.AccountID, 16), trunc(g.Symbol, 18), g.SaleDate.String(), acq, g.Quantity.String(), fmt.Sprint(g.HoldingDays),
			inr(g.SaleValue), inr(g.CostOfAcquisition), inr(g.ChargesDeductible), inr(g.Gain), gf}, widths, g.Gain)
	}
}

func (d *doc) tableHeader(cols []string, widths []float64) {
	d.pdf.SetFont("Helvetica", "B", 8)
	d.pdf.SetFillColor(232, 238, 242)
	d.pdf.SetTextColor(20, 20, 20)
	for i, c := range cols {
		d.pdf.CellFormat(widths[i], 6, c, "B", 0, "L", true, 0, "")
	}
	d.pdf.Ln(6)
}

func (d *doc) tableRow(cols []string, widths []float64, gain decimal.Decimal) {
	d.pdf.SetFont("Helvetica", "", 8)
	for i, c := range cols {
		align := "L"
		if strings.HasPrefix(c, "Rs") || strings.HasPrefix(c, "-Rs") || strings.HasPrefix(c, "+Rs") {
			align = "R"
		}
		d.pdf.SetTextColor(20, 20, 20)
		if i == len(cols)-2 && !gain.IsZero() {
			if gain.IsNegative() {
				d.pdf.SetTextColor(179, 38, 30)
			} else {
				d.pdf.SetTextColor(31, 122, 77)
			}
		}
		d.pdf.CellFormat(widths[i], 5.5, c, "", 0, align, false, 0, "")
	}
	d.pdf.Ln(5.5)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}

func pct(d decimal.Decimal) string { return d.Mul(decimal.NewFromInt(100)).String() + "%" }

// inr formats with Indian digit grouping and an explicit sign, e.g. "+Rs 1,25,000.00".
func inr(d decimal.Decimal) string {
	sign := ""
	if d.IsNegative() {
		sign = "-"
	}
	abs := d.Abs().StringFixed(2)
	whole, frac, _ := strings.Cut(abs, ".")
	if len(whole) > 3 {
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
		whole = strings.Join(parts, ",") + "," + last3
	}
	return sign + "Rs " + whole + "." + frac
}
