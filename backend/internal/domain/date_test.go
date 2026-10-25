package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAddMonthsClampsToMonthEnd(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"2024-01-31", 1, "2024-02-29"}, // leap year
		{"2023-01-31", 1, "2023-02-28"},
		{"2024-02-29", 12, "2025-02-28"},
		{"2024-08-10", 12, "2025-08-10"},
		{"2024-12-15", 1, "2025-01-15"},
		{"2024-03-31", -1, "2024-02-29"},
		{"2024-05-31", 1, "2024-06-30"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, MustDate(c.in).AddMonths(c.n).String(), "%s + %d months", c.in, c.n)
	}
}

func TestFinancialYear(t *testing.T) {
	require.Equal(t, "2024-25", FinancialYear(MustDate("2024-04-01")))
	require.Equal(t, "2024-25", FinancialYear(MustDate("2025-03-31")))
	require.Equal(t, "2025-26", FinancialYear(MustDate("2025-04-01")))
	require.Equal(t, "2023-24", FinancialYear(MustDate("2024-01-15")))
	require.Equal(t, "2099-00", FinancialYear(MustDate("2099-06-01")))
	a, b, err := FinancialYearBounds("2025-26")
	require.NoError(t, err)
	require.Equal(t, "2025-04-01", a.String())
	require.Equal(t, "2026-03-31", b.String())
	_, _, err = FinancialYearBounds("nonsense")
	require.Error(t, err)
}

func TestParseDateFormats(t *testing.T) {
	for _, s := range []string{"2024-08-10", "10-08-2024", "10/08/2024", "2024/08/10", "10-Aug-2024", "2024-08-10 15:04:05", "2024-08-10T15:04:05"} {
		d, err := ParseDate(s)
		require.NoError(t, err, s)
		require.Equal(t, NewDate(2024, time.August, 10), d, s)
	}
	_, err := ParseDate("not a date")
	require.Error(t, err)
}

func TestDateJSONRoundTrip(t *testing.T) {
	type wrap struct {
		D Date `json:"d"`
	}
	b, err := json.Marshal(wrap{MustDate("2025-07-01")})
	require.NoError(t, err)
	require.JSONEq(t, `{"d":"2025-07-01"}`, string(b))
	var w wrap
	require.NoError(t, json.Unmarshal([]byte(`{"d":"2025-07-01"}`), &w))
	require.Equal(t, MustDate("2025-07-01"), w.D)
	require.NoError(t, json.Unmarshal([]byte(`{"d":""}`), &w))
	require.True(t, w.D.IsZero())
}

func TestDaysUntilAndCompare(t *testing.T) {
	a, b := MustDate("2024-02-28"), MustDate("2024-03-01")
	require.Equal(t, 2, a.DaysUntil(b)) // 2024 is a leap year
	require.True(t, a.Before(b) && b.After(a) && a.Equal(a))
	require.Equal(t, -1, a.Compare(b))
}

func TestProRataSumsExactly(t *testing.T) {
	total := D("1000.0000")
	a := ProRata(total, D("1"), D("3"))
	b := ProRata(total, D("1"), D("3"))
	c := ProRata(total.Sub(a).Sub(b), D("1"), D("1")) // last allocation takes the remainder
	require.True(t, a.Add(b).Add(c).Equal(total))
	require.Equal(t, "333.3333", a.StringFixed(4))
}
