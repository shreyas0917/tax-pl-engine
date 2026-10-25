package domain

import (
	"github.com/shopspring/decimal"
)

// Money and quantities are shopspring decimals. Floating-point types are banned in this package
// and its siblings (enforced by TestNoFloatsInPurePackages).

// MoneyScale is the number of decimal places money is stored with (matches NUMERIC(20,4)).
const MoneyScale = 4

// QuantityScale matches NUMERIC(20,6); fractional units occur for mutual-fund units.
const QuantityScale = 6

// Zero is a reusable zero decimal.
var Zero = decimal.Zero

// D parses a decimal literal; it panics on a bad literal and is meant for constants and tests.
func D(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// RoundMoney rounds half away from zero to MoneyScale.
func RoundMoney(d decimal.Decimal) decimal.Decimal { return d.Round(MoneyScale) }

// RoundQty rounds to QuantityScale.
func RoundQty(d decimal.Decimal) decimal.Decimal { return d.Round(QuantityScale) }

// Max returns the larger of a and b.
func Max(a, b decimal.Decimal) decimal.Decimal {
	if a.GreaterThan(b) {
		return a
	}
	return b
}

// Min returns the smaller of a and b.
func Min(a, b decimal.Decimal) decimal.Decimal {
	if a.LessThan(b) {
		return a
	}
	return b
}

// ProRata allocates `total` to `part` out of `whole` with money rounding; when part == whole
// it returns total exactly so that a sequence of allocations always sums to the total.
func ProRata(total, part, whole decimal.Decimal) decimal.Decimal {
	if whole.IsZero() || part.Equal(whole) {
		return total
	}
	return RoundMoney(total.Mul(part).Div(whole))
}

// Ptr returns a pointer to d.
func Ptr(d decimal.Decimal) *decimal.Decimal { return &d }
