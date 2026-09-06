package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Money is represented everywhere in Go code as an int64 count of minor
// units (cents), never as float64. This makes arithmetic (debit/credit,
// balance checks) exact integer math with no rounding error.
//
// The database column is NUMERIC(20,2) as required by the assignment, so we
// need an exact, non-floating-point conversion at the boundary. These two
// functions do that conversion using only integer arithmetic and string
// formatting — at no point does a float enter the picture.

// CentsToNumeric formats cents as a NUMERIC(20,2)-compatible decimal string,
// e.g. 12345 -> "123.45", -50 -> "-0.50".
func CentsToNumeric(cents int64) string {
	neg := cents < 0
	abs := cents
	if neg {
		abs = -abs
	}
	whole := abs / 100
	frac := abs % 100
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, whole, frac)
}

// NumericToCents parses a NUMERIC(20,2) decimal string (as returned by
// lib/pq for a numeric column scanned into a string) into exact cents.
// It rejects more than 2 fractional digits rather than silently rounding.
func NumericToCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty numeric value")
	}

	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	parts := strings.SplitN(s, ".", 2)
	wholePart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}
	if len(fracPart) > 2 {
		return 0, fmt.Errorf("numeric value %q has more than 2 fractional digits", s)
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}

	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing whole part of %q: %w", s, err)
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing fractional part of %q: %w", s, err)
	}

	cents := whole*100 + frac
	if neg {
		cents = -cents
	}
	return cents, nil
}
