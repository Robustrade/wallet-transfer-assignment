package domain

import "regexp"

// MaxAmountMinor bounds a single transfer to a sane magnitude (10^15 minor
// units) so a malformed or malicious request can't overflow arithmetic on
// balances further down the pipeline.
const MaxAmountMinor int64 = 1_000_000_000_000_000

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// ValidAmount reports whether amount is a legal transfer amount: strictly
// positive minor units, no larger than MaxAmountMinor.
func ValidAmount(amountMinor int64) bool {
	return amountMinor > 0 && amountMinor <= MaxAmountMinor
}

// ValidCurrency reports whether code is a well-formed 3-letter ISO 4217 code.
// It does not validate against a currency registry — this service treats
// currency as an opaque matching key between wallets.
func ValidCurrency(code string) bool {
	return currencyPattern.MatchString(code)
}
