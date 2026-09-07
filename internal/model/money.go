package model

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	// CurrencyRegex matches non-negative decimal values with up to 2 optional decimal places
	CurrencyRegex = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,2})?$`)

	ErrInvalidAmountFormat = errors.New("amount must be a valid number with at most 2 decimal places")
	ErrAmountTooLarge      = errors.New("amount exceeds maximum allowable value")
)

// ParseToMinorUnits converts a string decimal (e.g. "10.50", "10.5", "10") into minor units (e.g. 1050 int64).
func ParseToMinorUnits(val string) (int64, error) {
	val = strings.TrimSpace(val)
	if !CurrencyRegex.MatchString(val) {
		return 0, ErrInvalidAmountFormat
	}

	parts := strings.Split(val, ".")
	wholeStr := parts[0]
	fractionStr := ""
	if len(parts) == 2 {
		fractionStr = parts[1]
	}

	// Pad right to exactly 2 digits: "5" -> "50", "" -> "00"
	for len(fractionStr) < 2 {
		fractionStr += "0"
	}

	whole, err := strconv.ParseInt(wholeStr, 10, 64)
	if err != nil {
		return 0, ErrAmountTooLarge
	}

	fraction, err := strconv.ParseInt(fractionStr, 10, 64)
	if err != nil {
		return 0, ErrInvalidAmountFormat
	}

	// Guard against int64 overflow on whole * 100
	const maxWhole = (math.MaxInt64 - 99) / 100
	if whole > maxWhole {
		return 0, ErrAmountTooLarge
	}

	return (whole * 100) + fraction, nil
}

// FormatMinorUnits formats an int64 minor unit balance into a string with 2 decimal places (e.g. 1050 -> "10.50").
func FormatMinorUnits(minorUnits int64) string {
	isNegative := minorUnits < 0
	if isNegative {
		minorUnits = -minorUnits
	}

	whole := minorUnits / 100
	fraction := minorUnits % 100

	sign := ""
	if isNegative {
		sign = "-"
	}

	return fmt.Sprintf("%s%d.%02d", sign, whole, fraction)
}
