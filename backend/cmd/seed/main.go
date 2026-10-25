// Command seed generates realistic synthetic Zerodha tradebooks for demos, tests and
// benchmarks. Output is deterministic for a given -seed so benchmarks are repeatable.
//
//	seed -out ./.data/seed -trades 500 -from 2023-04-01 -to 2026-03-31 -seed 1
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/shreyas0917/taxpl/internal/domain"
)

type scrip struct {
	symbol, isin string
	base         int64 // price in paise
}

// A small universe of well-known ISINs with plausible price levels.
var universe = []scrip{
	{"INFY", "INE009A01021", 145000}, {"TCS", "INE467B01029", 380000}, {"RELIANCE", "INE002A01018", 250000},
	{"HDFCBANK", "INE040A01034", 160000}, {"ITC", "INE154A01025", 42000}, {"SBIN", "INE062A01020", 75000},
	{"TATAMOTORS", "INE155A01022", 90000}, {"WIPRO", "INE075A01022", 45000}, {"BHARTIARTL", "INE397D01024", 120000},
	{"ICICIBANK", "INE090A01021", 110000}, {"HINDUNILVR", "INE030A01027", 240000}, {"LT", "INE018A01030", 340000},
	{"AXISBANK", "INE238A01034", 105000}, {"MARUTI", "INE585B01010", 1100000}, {"SUNPHARMA", "INE044A01036", 150000},
	{"TATASTEEL", "INE081A01020", 14000}, {"ONGC", "INE213A01029", 25000}, {"NTPC", "INE733E01010", 35000},
	{"POWERGRID", "INE752E01010", 28000}, {"COALINDIA", "INE522F01014", 40000},
}

func main() {
	out := flag.String("out", ".data/seed", "output directory")
	n := flag.Int("trades", 500, "approximate number of equity trades")
	fno := flag.Int("fno", 0, "number of F&O trades (0 = none)")
	from := flag.String("from", "2023-04-01", "first trade date")
	to := flag.String("to", "2026-03-31", "last trade date")
	seed := flag.Int64("seed", 1, "random seed")
	accounts := flag.Int("accounts", 1, "number of accounts (one file each)")
	flag.Parse()

	start, end := domain.MustDate(*from), domain.MustDate(*to)
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	rng := rand.New(rand.NewSource(*seed))
	for a := 1; a <= *accounts; a++ {
		name := filepath.Join(*out, fmt.Sprintf("zerodha_account%d_tradebook.csv", a))
		if err := writeEquity(name, rng, *n / *accounts, start, end); err != nil {
			fail(err)
		}
		fmt.Println("wrote", name)
		if *fno > 0 {
			name := filepath.Join(*out, fmt.Sprintf("zerodha_account%d_fno.csv", a))
			if err := writeFNO(name, rng, *fno / *accounts, start, end); err != nil {
				fail(err)
			}
			fmt.Println("wrote", name)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "seed:", err)
	os.Exit(1)
}

type row struct {
	date domain.Date
	t    string
	rec  []string
}

func writeEquity(path string, rng *rand.Rand, n int, start, end domain.Date) error {
	days := start.DaysUntil(end)
	holdings := map[int]int64{}
	var rows []row
	tradeID := int64(100000000 + rng.Intn(1000000))
	for i := 0; i < n; i++ {
		s := rng.Intn(len(universe))
		sc := universe[s]
		d := start.AddDays(rng.Intn(days + 1))
		if d.Time().Weekday() == 0 || d.Time().Weekday() == 6 {
			d = d.AddDays(2)
		}
		qty := int64(rng.Intn(50)+1) * 5
		side := "buy"
		if holdings[s] > 0 && rng.Intn(100) < 45 {
			side = "sell"
			if qty > holdings[s] {
				qty = holdings[s]
			}
		}
		drift := 1 + (rng.Float64()-0.45)*0.6 // crude price variation
		price := float64(sc.base) / 100 * drift
		if side == "buy" {
			holdings[s] += qty
		} else {
			holdings[s] -= qty
		}
		tradeID++
		ts := fmt.Sprintf("%sT%02d:%02d:%02d", d, 9+rng.Intn(6), rng.Intn(60), rng.Intn(60))
		rows = append(rows, row{date: d, t: ts, rec: []string{sc.symbol, sc.isin, d.String(), "NSE", "EQ", "EQ", side, "false",
			strconv.FormatInt(qty, 10), strconv.FormatFloat(price, 'f', 2, 64), strconv.FormatInt(tradeID, 10), strconv.FormatInt(tradeID*10, 10), ts}})
		// Occasionally add a same-day intraday round trip. The pair nets to zero delivery, so
		// the holdings tally is restored.
		if rng.Intn(100) < 8 {
			if side == "buy" {
				holdings[s] -= qty
			} else {
				holdings[s] += qty
			}
			tradeID++
			ts2 := fmt.Sprintf("%sT%02d:%02d:%02d", d, 12+rng.Intn(3), rng.Intn(60), rng.Intn(60))
			other := "sell"
			if side == "sell" {
				other = "buy"
			}
			rows = append(rows, row{date: d, t: ts2, rec: []string{sc.symbol, sc.isin, d.String(), "NSE", "EQ", "EQ", other, "false",
				strconv.FormatInt(qty, 10), strconv.FormatFloat(price*(1+(rng.Float64()-0.5)*0.02), 'f', 2, 64), strconv.FormatInt(tradeID, 10), strconv.FormatInt(tradeID*10, 10), ts2}})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].t < rows[j].t })
	return write(path, []string{"symbol", "isin", "trade_date", "exchange", "segment", "series", "trade_type", "auction", "quantity", "price", "trade_id", "order_id", "order_execution_time"}, rows)
}

func writeFNO(path string, rng *rand.Rand, n int, start, end domain.Date) error {
	days := start.DaysUntil(end)
	var rows []row
	tradeID := int64(200000000 + rng.Intn(1000000))
	for i := 0; i < n/2; i++ {
		d := start.AddDays(rng.Intn(days - 10))
		expiry := d.AddDays(7 + rng.Intn(20))
		strike := 22000 + rng.Intn(40)*100
		ot := "CE"
		if rng.Intn(2) == 0 {
			ot = "PE"
		}
		mon := expiry.Time().Format("Jan")
		sym := fmt.Sprintf("NIFTY%02d%s%d%s", expiry.Year%100, toUpper(mon), strike, ot)
		qty := int64(25 * (1 + rng.Intn(4)))
		open := 50 + rng.Float64()*200
		closeP := open * (0.5 + rng.Float64())
		sides := []string{"buy", "sell"}
		if rng.Intn(2) == 0 {
			sides = []string{"sell", "buy"}
		}
		for k, side := range sides {
			tradeID++
			dd := d.AddDays(k * (1 + rng.Intn(5)))
			if dd.After(expiry) {
				dd = expiry
			}
			p := open
			if k == 1 {
				p = closeP
			}
			ts := fmt.Sprintf("%sT%02d:%02d:%02d", dd, 9+rng.Intn(6), rng.Intn(60), rng.Intn(60))
			rows = append(rows, row{date: dd, t: ts, rec: []string{sym, "", dd.String(), "NFO", "FO", ot, side, "false",
				strconv.FormatInt(qty, 10), strconv.FormatFloat(p, 'f', 2, 64), strconv.FormatInt(tradeID, 10), strconv.FormatInt(tradeID*10, 10), ts, expiry.String()}})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].t < rows[j].t })
	return write(path, []string{"symbol", "isin", "trade_date", "exchange", "segment", "series", "trade_type", "auction", "quantity", "price", "trade_id", "order_id", "order_execution_time", "expiry_date"}, rows)
}

func toUpper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

func write(path string, header []string, rows []row) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		_ = f.Close()
		return err
	}
	for _, r := range rows {
		if err := w.Write(r.rec); err != nil {
			_ = f.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
