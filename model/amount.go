package model

import (
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
)

// NewAmount constructs a transfer/ledger amount. Zero and negative values are rejected.
func NewAmount(v int64) (helpers.Money, error) {
	if v <= 0 {
		return 0, ErrInvalidAmount
	}
	return helpers.Money(v), nil
}

// NewBalance constructs a wallet balance. Zero is allowed; negatives are not.
func NewBalance(v int64) (helpers.Money, error) {
	if v < 0 {
		return 0, ErrNegativeBalance
	}
	return helpers.Money(v), nil
}

// NormalizeID trims surrounding whitespace. Empty after trim is invalid.
func NormalizeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrInvalidWalletID
	}
	return id, nil
}

// NormalizeIdempotencyKey trims surrounding whitespace. Empty after trim is invalid.
func NormalizeIdempotencyKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ErrInvalidIdempotencyKey
	}
	return key, nil
}
