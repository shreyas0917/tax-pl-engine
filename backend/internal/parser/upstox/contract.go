package upstox

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shreyas0917/taxpl/internal/domain"
)

// contract is a decoded derivative name.
type contract struct {
	underlying string
	expiry     domain.Date // zero when the name carries only a month (NSE trading-symbol style)
	strike     decimal.Decimal
	optType    string
	segment    domain.Segment
}

var (
	// tokenRe splits "NIFTY 27JUN24 23000 CE", "NIFTY27JUN2423000CE", "BAJAJ-AUTO 27-JUN-2024 FUT"
	// into letter runs (hyphens allowed inside a word) and number runs.
	tokenRe    = regexp.MustCompile(`[A-Z&]+(?:-[A-Z&]+)*|\d+(?:\.\d+)?`)
	monthIndex = map[string]time.Month{
		"JAN": time.January, "FEB": time.February, "MAR": time.March, "APR": time.April,
		"MAY": time.May, "JUN": time.June, "JUL": time.July, "AUG": time.August,
		"SEP": time.September, "OCT": time.October, "NOV": time.November, "DEC": time.December,
	}
)

// decodeContract recognises the common ways Indian brokers print an F&O contract:
//
//	NIFTY 27JUN24 23000 CE      NIFTY27JUN2423000CE      NIFTY 27-JUN-2024 CE 23000
//	BANKNIFTY 26JUN2024 FUT     NIFTY24JUN23000CE (monthly, expiry must come from a column)
func decodeContract(sym string) (contract, bool) {
	toks := tokens(sym)
	mi := -1
	for i := 1; i < len(toks)-1; i++ {
		if _, ok := monthIndex[toks[i]]; ok && isInt(toks[i-1]) && len(toks[i-1]) <= 2 {
			mi = i
			break
		}
	}
	if mi < 1 {
		return contract{}, false
	}
	und := strings.Join(toks[:mi-1], "")
	if und == "" || und[0] < 'A' || und[0] > 'Z' {
		return contract{}, false
	}
	month := monthIndex[toks[mi]]
	rest := toks[mi+1:]
	// Full form "<dd><MMM><yy|yyyy> ...": the year may be glued to the strike (27JUN24 23000 -> 2423000).
	if isInt(rest[0]) {
		day, _ := strconv.Atoi(toks[mi-1])
		yearTok, strikeTok, tail := rest[0], "", rest[1:]
		if len(yearTok) > 4 {
			yearTok, strikeTok = yearTok[:2], yearTok[2:]
		}
		if y, ok := year(yearTok); ok {
			if d, ok := civil(y, month, day); ok {
				if c, ok := finish(und, d, strikeTok, tail); ok {
					return c, true
				}
			}
		}
	}
	// Monthly form "<yy><MMM>..." carries no day.
	if len(toks[mi-1]) == 2 {
		return finish(und, domain.Date{}, "", rest)
	}
	return contract{}, false
}

// tokens splits the name into letter and number runs; a glued "JUNFUT" becomes "JUN", "FUT".
func tokens(sym string) []string {
	raw := tokenRe.FindAllString(strings.ToUpper(strings.TrimSpace(sym)), -1)
	out := make([]string, 0, len(raw)+1)
	for _, t := range raw {
		if len(t) > 3 {
			if _, ok := monthIndex[t[:3]]; ok && (isFutToken(t[3:]) || optToken(t[3:]) != "") {
				out = append(out, t[:3], t[3:])
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// finish reads "[strike] CE|PE", "CE|PE [strike]" or "FUT".
func finish(und string, expiry domain.Date, strikeTok string, tail []string) (contract, bool) {
	c := contract{underlying: und, expiry: expiry}
	switch {
	case len(tail) == 1 && strikeTok == "" && isFutToken(tail[0]):
		c.segment = domain.SegFuture
		return c, true
	case len(tail) == 1 && strikeTok != "" && optToken(tail[0]) != "":
		return option(c, strikeTok, tail[0])
	case len(tail) == 2 && strikeTok == "" && optToken(tail[1]) != "" && isNumber(tail[0]):
		return option(c, tail[0], tail[1])
	case len(tail) == 2 && strikeTok == "" && optToken(tail[0]) != "" && isNumber(tail[1]):
		return option(c, tail[1], tail[0])
	}
	return contract{}, false
}

func option(c contract, strikeTok, typ string) (contract, bool) {
	s, err := decimal.NewFromString(strikeTok)
	if err != nil || !s.IsPositive() {
		return contract{}, false
	}
	c.strike = s
	c.optType = optToken(typ)
	c.segment = domain.SegOption
	return c, true
}

// optToken normalises CE/PE/CALL/PUT to CE or PE ("" when not an option token).
func optToken(s string) string {
	switch strings.ToUpper(s) {
	case "CE", "CALL":
		return "CE"
	case "PE", "PUT":
		return "PE"
	}
	return ""
}

func isFutToken(s string) bool {
	switch strings.ToUpper(s) {
	case "FUT", "FUTURE", "FUTURES", "FUTIDX", "FUTSTK":
		return true
	}
	return false
}

func year(tok string) (int, bool) {
	y, err := strconv.Atoi(tok)
	if err != nil {
		return 0, false
	}
	switch len(tok) {
	case 2:
		return 2000 + y, true
	case 4:
		return y, y >= 1990 && y <= 2100
	}
	return 0, false
}

func civil(y int, m time.Month, d int) (domain.Date, bool) {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || t.Month() != m {
		return domain.Date{}, false
	}
	return domain.DateOf(t), true
}

func isInt(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
