package domain

import (
	"math"
	"regexp"
	"strings"
)

type Amount int64

const MaxAmount Amount = 1_000_000_000_000

const maxWalletIDLength = 64
const maxIdempotencyKeyLength = 255

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func ValidateAmount(a Amount) error {
	switch {
	case a <= 0:
		return newValidationError("amount", "must be a positive integer in minor units")
	case a > MaxAmount:
		return newValidationError("amount", "exceeds the maximum permitted transfer amount")
	}
	return nil
}

func CanAdd(balance, delta Amount) bool {
	if delta > 0 && balance > Amount(math.MaxInt64)-delta {
		return false
	}
	if delta < 0 && balance < Amount(math.MinInt64)-delta {
		return false
	}
	return true
}

func NormalizeWalletID(id string) string { return strings.TrimSpace(id) }

func ValidateWalletID(field, id string) error {
	switch {
	case id == "":
		return newValidationError(field, "must not be empty")
	case len(id) > maxWalletIDLength:
		return newValidationError(field, "must be at most 64 characters")
	}
	return nil
}

func ValidateIdempotencyKey(key string) error {
	switch {
	case strings.TrimSpace(key) == "":
		return newValidationError("idempotencyKey", "must not be empty")
	case len(key) > maxIdempotencyKeyLength:
		return newValidationError("idempotencyKey", "must be at most 255 characters")
	}
	return nil
}

func ValidateCurrency(code string) error {
	if !currencyPattern.MatchString(code) {
		return newValidationError("currency", "must be a three-letter uppercase ISO 4217 code")
	}
	return nil
}
