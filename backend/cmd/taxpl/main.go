// Command taxpl is the command-line interface to the Tax P&L Engine.
//
//	taxpl calculate --fy 2025-26 [--out out.json] [--ref ref.json] [account=]file.csv ...
//	taxpl detect file.csv
//	taxpl rules
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/parser"
	_ "github.com/shreyas0917/taxpl/internal/parser/all"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/tax"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "calculate":
		err = calculate(os.Args[2:])
	case "detect":
		err = detect(os.Args[2:])
	case "rules":
		err = rules()
	case "-h", "--help", "help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "taxpl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  taxpl calculate --fy 2025-26 [--out out.json] [--ref ref.json] [--broker zerodha] [account=]file.csv ...
  taxpl detect file.csv
  taxpl rules

Each file is bound to a demat account. Prefix the path with "account=" to name it; the default
is the file name without extension. ref.json may hold instruments (with 31-Jan-2018 FMVs),
corporate actions, opening lots and extra trades (transfers, buybacks).`)
}

// RefFile is the optional reference-data file for the CLI.
type RefFile struct {
	Instruments      []domain.Instrument      `json:"instruments"`
	SymbolToISIN     map[string]string        `json:"symbol_to_isin"`
	CorporateActions []domain.CorporateAction `json:"corporate_actions"`
	OpeningLots      []domain.Lot             `json:"opening_lots"`
	ExtraTrades      []domain.Trade           `json:"extra_trades"`
	AsOf             domain.Date              `json:"as_of"`
}

func calculate(args []string) error {
	fs := flag.NewFlagSet("calculate", flag.ContinueOnError)
	fy := fs.String("fy", "", "financial year, e.g. 2025-26 (default: all years in the data)")
	out := fs.String("out", "out.json", "output JSON path")
	ref := fs.String("ref", "", "reference data JSON (instruments, corporate actions, opening lots)")
	broker := fs.String("broker", "", "force a broker parser instead of auto-detecting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("no input files")
	}
	req := pipeline.Request{FY: *fy, Instruments: map[string]domain.Instrument{}}
	if *ref != "" {
		b, err := os.ReadFile(*ref)
		if err != nil {
			return err
		}
		var rf RefFile
		if err := json.Unmarshal(b, &rf); err != nil {
			return fmt.Errorf("ref file: %w", err)
		}
		for _, inst := range rf.Instruments {
			req.Instruments[inst.ISIN] = inst
		}
		req.SymbolToISIN = rf.SymbolToISIN
		req.CorporateActions = rf.CorporateActions
		req.OpeningLots = rf.OpeningLots
		req.ExtraTrades = rf.ExtraTrades
		req.AsOf = rf.AsOf
	}
	for _, arg := range fs.Args() {
		account, path := splitAccount(arg)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		req.Files = append(req.Files, pipeline.File{Name: filepath.Base(path), AccountID: account, Broker: *broker, Data: data})
	}
	rep, err := pipeline.Run(req)
	if err != nil {
		return err
	}
	b, err := jsonreport.Marshal(rep)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	printSummary(rep)
	fmt.Printf("\nWrote %s (sha256 %s)\n%s\n", *out, jsonreport.Checksum(b)[:16], pipeline.Disclaimer)
	return nil
}

// splitAccount handles "account=path" (a Windows drive letter is not an account).
func splitAccount(arg string) (string, string) {
	if i := strings.Index(arg, "="); i > 0 {
		return arg[:i], arg[i+1:]
	}
	base := filepath.Base(arg)
	return strings.TrimSuffix(base, filepath.Ext(base)), arg
}

func printSummary(rep *pipeline.Report) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "FILE\tACCOUNT\tBROKER\tROWS\tTRADES\tISSUES")
	for _, f := range rep.Files {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s (%.0f%%)\t%d\t%d\t%d\n", f.Name, f.AccountID, f.Broker, f.Confidence*100, f.Rows, f.Trades, len(f.Issues))
	}
	_ = w.Flush()
	for _, s := range rep.Summaries {
		fmt.Printf("\n%s  [%s, rules %s]\n", s.YearLabel, s.Law, s.RulesVersion)
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', tabwriter.AlignRight)
		_, _ = fmt.Fprintln(w, "CATEGORY\tLINES\tSALE VALUE\tCOST\tCHARGES\tGAIN\t")
		for _, c := range s.Categories {
			_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\t%s\t\n", c.Category, c.Count, money(c.SaleValue), money(c.Cost), money(c.Charges), signed(c.Gain))
		}
		_ = w.Flush()
		fmt.Printf("  STCG %s | LTCG %s | Intraday %s | F&O %s | F&O turnover %s\n", signed(s.STCG), signed(s.LTCG), signed(s.Intraday), signed(s.FNO), money(s.FNOTurnover))
		if !s.BuybackDividend.IsZero() || !s.BuybackExempt.IsZero() {
			fmt.Printf("  Buyback: deemed dividend %s | exempt (s.115QA era) %s\n", money(s.BuybackDividend), money(s.BuybackExempt))
		}
		for _, p := range s.Periods {
			if len(s.Periods) > 1 {
				fmt.Printf("  period %s: STCG %s @%s%% | LTCG %s @%s%%\n", p.Period, signed(p.STCG), pct(p.STCGRate), signed(p.LTCG), pct(p.LTCGRate))
			}
		}
		fmt.Printf("  Estimated tax on equity gains (estimate only): %s\n", money(s.EstimatedTax))
		for _, l := range s.Losses {
			fmt.Printf("  Loss %s %s: set off against %s; carry forward %d years against %s\n", l.Loss, money(l.Amount), strings.Join(l.SetOffAgainst, "/"), l.CarryForwardYears, strings.Join(l.CarryForwardAgainst, "/"))
		}
	}
	if len(rep.Review) > 0 {
		fmt.Printf("\nNEEDS REVIEW (%d):\n", len(rep.Review))
		for _, r := range rep.Review {
			fmt.Printf("  - [%s] %s\n", r.Kind, r.Message)
		}
	}
}

func money(d decimal.Decimal) string { return d.StringFixed(2) }

func signed(d decimal.Decimal) string {
	if d.IsNegative() {
		return "-" + d.Abs().StringFixed(2)
	}
	return "+" + d.StringFixed(2)
}

func pct(d decimal.Decimal) string { return d.Mul(decimal.NewFromInt(100)).String() }

func detect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("detect takes one file")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	for _, d := range parser.Detect(data) {
		fmt.Printf("%-10s %.0f%%\n", d.Broker, d.Confidence*100)
	}
	return nil
}

func rules() error {
	for _, y := range tax.Years() {
		r, err := tax.Load(y)
		if err != nil {
			return err
		}
		verified := "NOT YET VERIFIED BY A CA"
		if r.VerifiedBy != "" {
			verified = "verified by " + r.VerifiedBy
		}
		fmt.Printf("%s  %s  %s  (%s)\n", r.VersionString(), r.YearLabel, r.Law, verified)
		for _, p := range r.Equity.Periods {
			fmt.Printf("    %s  STCG %s%%  LTCG %s%%  exemption %s\n", p.Label(), pct(p.STCGRate), pct(p.LTCGRate), r.Equity.LTCGExemption)
		}
	}
	return nil
}
