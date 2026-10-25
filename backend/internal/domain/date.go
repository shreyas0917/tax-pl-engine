package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Date is a civil date (no time, no zone). All tax rules operate on civil dates:
// the trade date decides the financial year, the holding period and the rate period.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// NewDate builds a Date; it does not normalise invalid dates.
func NewDate(y int, m time.Month, d int) Date { return Date{Year: y, Month: m, Day: d} }

// DateOf truncates a time.Time to its civil date in the time's own location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

var dateLayouts = []string{
	"2006-01-02", "02-01-2006", "02/01/2006", "2006/01/02", "02-Jan-2006", "2 Jan 2006",
	"Jan 2, 2006", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "02-Jan-06", "2-Jan-2006",
}

// ParseDate accepts the formats brokers commonly emit.
func ParseDate(s string) (Date, error) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return DateOf(t), nil
		}
	}
	return Date{}, fmt.Errorf("unrecognised date %q", s)
}

// MustDate panics on a bad date; for tests and constants only.
func MustDate(s string) Date {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// Time returns the date as a UTC midnight instant.
func (d Date) Time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// String formats as YYYY-MM-DD.
func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// IsZero reports whether the date is unset.
func (d Date) IsZero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// Compare returns -1, 0 or +1.
func (d Date) Compare(o Date) int {
	switch {
	case d.Year != o.Year:
		return cmpInt(d.Year, o.Year)
	case d.Month != o.Month:
		return cmpInt(int(d.Month), int(o.Month))
	default:
		return cmpInt(d.Day, o.Day)
	}
}

// Before reports d < o.
func (d Date) Before(o Date) bool { return d.Compare(o) < 0 }

// After reports d > o.
func (d Date) After(o Date) bool { return d.Compare(o) > 0 }

// Equal reports d == o.
func (d Date) Equal(o Date) bool { return d.Compare(o) == 0 }

// AddDays returns the date n days later (negative n goes back).
func (d Date) AddDays(n int) Date { return DateOf(d.Time().AddDate(0, 0, n)) }

// AddMonths adds calendar months, clamping to the last valid day of the target
// month (31 Jan + 1 month = 28/29 Feb). Go's AddDate would roll over instead.
func (d Date) AddMonths(n int) Date {
	y, m := d.Year, int(d.Month)+n
	for m > 12 {
		m -= 12
		y++
	}
	for m < 1 {
		m += 12
		y--
	}
	last := time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	day := d.Day
	if day > last {
		day = last
	}
	return Date{Year: y, Month: time.Month(m), Day: day}
}

// DaysUntil returns the number of days from d to o (o - d).
func (d Date) DaysUntil(o Date) int { return int(o.Time().Sub(d.Time()).Hours() / 24) }

// MarshalJSON encodes as "YYYY-MM-DD".
func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// UnmarshalJSON decodes "YYYY-MM-DD".
func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	return d.UnmarshalText([]byte(s))
}

// MarshalText lets Date be used as a map key and in YAML.
func (d Date) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText parses "YYYY-MM-DD"; empty text is the zero date.
func (d *Date) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = Date{}
		return nil
	}
	p, err := ParseDate(string(b))
	if err != nil {
		return err
	}
	*d = p
	return nil
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// FinancialYear returns the Indian financial year label ("2025-26") that a date falls in.
// 1 April to 31 March. Works for both the 1961 Act (previous year) and the 2025 Act (tax year).
func FinancialYear(d Date) string {
	start := d.Year
	if d.Month < time.April {
		start = d.Year - 1
	}
	return fmt.Sprintf("%d-%02d", start, (start+1)%100)
}

// FinancialYearBounds returns the first and last day of the financial year label "2025-26".
func FinancialYearBounds(fy string) (Date, Date, error) {
	var start, end int
	if _, err := fmt.Sscanf(fy, "%d-%d", &start, &end); err != nil || start < 1900 || start > 2200 {
		return Date{}, Date{}, fmt.Errorf("bad financial year %q (want e.g. 2025-26)", fy)
	}
	return NewDate(start, time.April, 1), NewDate(start+1, time.March, 31), nil
}
