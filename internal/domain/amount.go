package domain

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// Amount is a fixed-point decimal string with exactly 4 decimal places (e.g. "100.0000").
// Using a string avoids float precision issues when crossing JSON and DB boundaries.
type Amount string

// decimalRe matches a plain, optionally-signed decimal number such as "100",
// "100.5", or "-0.0001". It deliberately excludes formats that big.Rat.SetString
// would otherwise accept — fractions ("1/3"), exponents ("1e3"), and surrounding
// whitespace — none of which are valid amounts in our API contract.
var decimalRe = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?$`)

// ParseAmount parses a decimal string into an Amount, rejecting negative values.
func ParseAmount(s string) (Amount, error) {
	if !decimalRe.MatchString(s) {
		return "", fmt.Errorf("invalid amount %q: must be a plain decimal number", s)
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return "", fmt.Errorf("invalid amount %q: not a decimal number", s)
	}
	if r.Sign() < 0 {
		return "", fmt.Errorf("invalid amount %q: must not be negative", s)
	}
	return Amount(ratToString(r)), nil
}

// Rat returns the Amount as a *big.Rat for arithmetic.
func (a Amount) Rat() *big.Rat {
	r, _ := new(big.Rat).SetString(string(a))
	return r
}

// IsPositive reports whether a > 0.
func (a Amount) IsPositive() bool {
	return a.Rat().Sign() > 0
}

// GTE reports whether a >= b.
func (a Amount) GTE(b Amount) bool {
	return a.Rat().Cmp(b.Rat()) >= 0
}

// Add returns a + b.
func (a Amount) Add(b Amount) Amount {
	return Amount(ratToString(new(big.Rat).Add(a.Rat(), b.Rat())))
}

// Sub returns a - b.
func (a Amount) Sub(b Amount) Amount {
	return Amount(ratToString(new(big.Rat).Sub(a.Rat(), b.Rat())))
}

func (a Amount) String() string { return string(a) }

// RatToAmount converts a *big.Rat to an Amount with 4 decimal places.
func RatToAmount(r *big.Rat) Amount {
	return Amount(ratToString(r))
}

// ratToString formats a *big.Rat to a decimal string with exactly 4 decimal places.
func ratToString(r *big.Rat) string {
	scaled := new(big.Rat).Mul(r, big.NewRat(10000, 1))
	intPart := new(big.Int).Quo(scaled.Num(), scaled.Denom())

	s := intPart.String()
	negative := strings.HasPrefix(s, "-")
	if negative {
		s = s[1:]
	}
	for len(s) <= 4 {
		s = "0" + s
	}
	result := s[:len(s)-4] + "." + s[len(s)-4:]
	if negative {
		return "-" + result
	}
	return result
}
