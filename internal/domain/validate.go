package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"regexp"
	"strconv"
)

// token matches the assignment examples, such as wallet_1, and rejects spaces and null bytes.
var token = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidateWalletID checks a wallet id before any database call.
func ValidateWalletID(id string) error {
	if len(id) < 1 || len(id) > 64 || !token.MatchString(id) {
		return &InputError{Message: "wallet id must be 1 to 64 letters, digits, underscores, or hyphens"}
	}
	return nil
}

// ValidateTransfer checks a transfer before the service claims an idempotency key.
func ValidateTransfer(fromWalletID, toWalletID, idempotencyKey string, amount int64) error {
	if len(idempotencyKey) < 1 || len(idempotencyKey) > 128 || !token.MatchString(idempotencyKey) {
		return &InputError{Message: "idempotencyKey must be 1 to 128 letters, digits, underscores, or hyphens"}
	}
	if err := ValidateWalletID(fromWalletID); err != nil {
		return &InputError{Message: "fromWalletId must be 1 to 64 letters, digits, underscores, or hyphens"}
	}
	if err := ValidateWalletID(toWalletID); err != nil {
		return &InputError{Message: "toWalletId must be 1 to 64 letters, digits, underscores, or hyphens"}
	}
	if fromWalletID == toWalletID {
		return &InputError{Message: "fromWalletId and toWalletId must differ"}
	}
	if amount <= 0 {
		return &InputError{Message: "amount must be a positive integer"}
	}
	return nil
}

// CreditOverflow reports whether balance + amount exceeds the signed 64-bit range.
func CreditOverflow(balance, amount int64) bool {
	if amount <= 0 {
		return true
	}
	return balance > math.MaxInt64-amount
}

// Fingerprint identifies a transfer payload independent of JSON field order.
func Fingerprint(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(fromWalletID + "\n" + toWalletID + "\n" + strconv.FormatInt(amount, 10)))
	return hex.EncodeToString(sum[:])
}
