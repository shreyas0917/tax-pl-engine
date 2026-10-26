// Command bench measures the pure pipeline on synthetic tradebooks and prints a Markdown
// table for docs/benchmarks.md.
//
//	bench -trades 100000,1000000 -accounts 3 -isins 200 [-cpuprofile pipeline.prof]
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
	"github.com/shreyas0917/taxpl/internal/matcher"
	"github.com/shreyas0917/taxpl/internal/pipeline"
	"github.com/shreyas0917/taxpl/internal/report/jsonreport"
	"github.com/shreyas0917/taxpl/internal/tax"
)

func main() {
	sizes := flag.String("trades", "100000,1000000", "comma-separated trade counts")
	accounts := flag.Int("accounts", 3, "number of demat accounts")
	isins := flag.Int("isins", 200, "number of distinct ISINs")
	prof := flag.String("cpuprofile", "", "write a CPU profile of the largest run")
	flag.Parse()

	fmt.Println("| Trades | Accounts | ISINs | Generate | Match (FIFO + netting) | Classify + summarise | Full pipeline | JSON report | Peak heap |")
	fmt.Println("|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	parts := strings.Split(*sizes, ",")
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			fmt.Fprintln(os.Stderr, "bad size", p)
			os.Exit(2)
		}
		if *prof != "" && i == len(parts)-1 {
			f, err := os.Create(*prof)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			_ = pprof.StartCPUProfile(f)
			run(n, *accounts, *isins)
			pprof.StopCPUProfile()
			_ = f.Close()
			continue
		}
		run(n, *accounts, *isins)
	}
}

func run(n, accounts, isins int) {
	runtime.GC()
	t0 := time.Now()
	trades := generate(n, accounts, isins, 42)
	tGen := time.Since(t0)

	rules, _ := tax.Load("2025-26")
	cfg := tax.ConfigFor(rules)

	runtime.GC()
	t1 := time.Now()
	res, err := matcher.Match(matcher.Input{Trades: trades, Config: cfg})
	if err != nil {
		panic(err)
	}
	tMatch := time.Since(t1)

	t2 := time.Now()
	cls := tax.Classify(res)
	for _, fy := range tax.Years() {
		_, _ = tax.Summarise(fy, cls.Gains)
	}
	tClassify := time.Since(t2)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	t3 := time.Now()
	rep, err := pipeline.Run(pipeline.Request{ExtraTrades: trades})
	if err != nil {
		panic(err)
	}
	tPipeline := time.Since(t3)
	runtime.ReadMemStats(&after)

	t4 := time.Now()
	b, err := jsonreport.Marshal(rep)
	if err != nil {
		panic(err)
	}
	tJSON := time.Since(t4)

	fmt.Printf("| %s | %d | %d | %s | %s | %s | %s | %s (%s) | %s |\n", group(n), accounts, isins, round(tGen), round(tMatch), round(tClassify), round(tPipeline),
		round(tJSON), humanBytes(len(b)), humanBytes(int(after.HeapAlloc-before.HeapAlloc)))
	fmt.Fprintf(os.Stderr, "  %d trades -> %d lots, %d matches, %d intraday pairs, %d gains, %d review items\n", n, len(res.Lots), len(res.Matches), len(res.Intraday), len(cls.Gains), len(res.Review))
}

func round(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2f s", d.Seconds())
}

func humanBytes(n int) string {
	switch {
	case n > 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n>>10)
}

func group(n int) string {
	s := strconv.Itoa(n)
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, out...), ",")
}

// generate builds a realistic multi-year history: random buys, sells capped at holdings,
// an 8% intraday round-trip rate, across accounts and ISINs.
func generate(n, accounts, isins int, seed int64) []domain.Trade {
	rng := rand.New(rand.NewSource(seed))
	start := domain.MustDate("2022-04-01")
	holdings := map[[2]int]int64{}
	out := make([]domain.Trade, 0, n)
	for i := 0; i < n; i++ {
		a, s := rng.Intn(accounts), rng.Intn(isins)
		key := [2]int{a, s}
		day := start.AddDays(i * 1200 / n) // monotonic dates, ~1200 days span
		qty := int64(rng.Intn(50)+1) * 5
		side := domain.Buy
		if holdings[key] > 0 && rng.Intn(100) < 45 {
			side = domain.Sell
			if qty > holdings[key] {
				qty = holdings[key]
			}
		}
		if side == domain.Buy {
			holdings[key] += qty
		} else {
			holdings[key] -= qty
		}
		price := decimal.NewFromInt(int64(100 + s*7 + rng.Intn(50)))
		id := fmt.Sprintf("t%09d", i)
		out = append(out, domain.Trade{ID: id, AccountID: "acct" + strconv.Itoa(a), ISIN: fmt.Sprintf("INE%06dA01", s), Symbol: fmt.Sprintf("S%d", s), Segment: domain.SegEquity,
			Kind: domain.KindRegular, Side: side, Quantity: decimal.NewFromInt(qty), Price: price, TradeDate: day, TradeTime: fmt.Sprintf("%02d:%02d:00", 9+rng.Intn(6), rng.Intn(60)), TradeRef: id})
		if rng.Intn(100) < 8 && len(out) < n {
			other := domain.Sell
			if side == domain.Sell {
				other = domain.Buy
			}
			id2 := fmt.Sprintf("t%09dx", i)
			out = append(out, domain.Trade{ID: id2, AccountID: "acct" + strconv.Itoa(a), ISIN: fmt.Sprintf("INE%06dA01", s), Symbol: fmt.Sprintf("S%d", s), Segment: domain.SegEquity,
				Kind: domain.KindRegular, Side: other, Quantity: decimal.NewFromInt(qty), Price: price.Add(decimal.NewFromInt(int64(rng.Intn(5) - 2))), TradeDate: day, TradeTime: "15:00:00", TradeRef: id2})
			if other == domain.Buy {
				holdings[key] += qty
			} else {
				holdings[key] -= qty
			}
		}
	}
	return out
}
