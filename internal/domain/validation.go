package domain

import (
	"math/big"

	"github.com/google/uuid"
)

// ValidateTransferRequest validates a transfer request input
func ValidateTransferRequest(req *TransferRequest) error {
	// Validate idempotency key
	if req.IdempotencyKey == "" {
		return ErrInvalidIdempotencyKey
	}
	if len(req.IdempotencyKey) > 255 {
		return ErrInvalidIdempotencyKey
	}

	// Validate wallet IDs are valid UUIDs
	if _, err := uuid.Parse(req.FromWalletID); err != nil {
		return ErrInvalidWalletID
	}
	if _, err := uuid.Parse(req.ToWalletID); err != nil {
		return ErrInvalidWalletID
	}

	// Validate amount
	if req.Amount == "" {
		return ErrInvalidAmountFormat
	}

	amount := new(big.Float)
	if _, ok := amount.SetString(req.Amount); !ok {
		return ErrInvalidAmountFormat
	}

	if amount.Cmp(big.NewFloat(0)) <= 0 {
		return ErrInvalidAmount
	}

	return nil
}
