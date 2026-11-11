package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
	appErrors "wallet-transfer-assignment/internal/appErrors"
)

// MaxIdentifierLength matches the database's wallet ID and idempotency key limits,
// so oversized values receive a validation error before a database write is attempted.
const MaxIdentifierLength = 128

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r TransferRequest) Validate(maxAmount int64) error {
	for _, field := range []struct{ name, value string }{
		{"idempotencyKey", r.IdempotencyKey},
		{"fromWalletId", r.FromWalletID},
		{"toWalletId", r.ToWalletID},
	} {
		if strings.TrimSpace(field.value) == "" || utf8.RuneCountInString(field.value) > MaxIdentifierLength {
			return &appErrors.ValidationError{Field: field.name, Reason: "must be nonblank and at most 128 characters"}
		}
	}
	if r.FromWalletID == r.ToWalletID {
		return &appErrors.ValidationError{Field: "toWalletId", Reason: "must differ from fromWalletId"}
	}
	if r.Amount <= 0 || r.Amount > maxAmount {
		return &appErrors.ValidationError{Field: "amount", Reason: "must be positive and within the configured transfer limit"}
	}
	return nil
}

// Hash is unambiguous even when wallet IDs have different field boundaries.
// It excludes the key and preserves case; callers must validate before reserving.
func (r TransferRequest) Hash() string {
	payload := fmt.Sprintf("%d:%s%d:%s%d", len(r.FromWalletID), r.FromWalletID, len(r.ToWalletID), r.ToWalletID, r.Amount)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}
