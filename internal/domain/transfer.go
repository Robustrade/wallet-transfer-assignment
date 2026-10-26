package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const maxIdentifierLength = 128

var (
	ErrInvalidIdempotencyKey = errors.New("idempotency key must be 1-128 non-whitespace characters")
	ErrInvalidWalletID       = errors.New("wallet IDs must be 1-128 non-whitespace characters")
	ErrSameWallet            = errors.New("source and destination wallets must differ")
	ErrInvalidAmount         = errors.New("amount must be a positive integer in minor currency units")
)

// TransferRequest contains only the data needed to identify a logical transfer.
// Wallet existence is intentionally NOT checked here: existing idempotency keys
// must be resolved before wallet-dependent validation.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// ValidateStructure does not access the database or mutate/canonicalize IDs.
func (r TransferRequest) ValidateStructure() error {
	if !validIdentifier(r.IdempotencyKey) {
		return ErrInvalidIdempotencyKey
	}
	if !validIdentifier(r.FromWalletID) || !validIdentifier(r.ToWalletID) {
		return ErrInvalidWalletID
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

func validIdentifier(s string) bool {
	return len(s) > 0 && utf8.ValidString(s) && utf8.RuneCountInString(s) <= maxIdentifierLength && strings.TrimSpace(s) == s && !strings.ContainsRune(s, '\x00')
}

// SameOperation checks a committed key's immutable transfer parameters.
// It is meaningful only once the idempotency key has already been found.
func (r TransferRequest) SameOperation(existing TransferRequest) bool {
	return r.IdempotencyKey == existing.IdempotencyKey &&
		r.FromWalletID == existing.FromWalletID &&
		r.ToWalletID == existing.ToWalletID &&
		r.Amount == existing.Amount
}
