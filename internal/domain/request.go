package domain

import "github.com/google/uuid"

// CreateTransferRequest is the validated, parsed form of a POST /transfers
// request body. The handler is responsible for JSON parsing; this package
// is responsible for the business validation rules.
type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64 // minor units (cents)
}

// Validate applies the request-level business rules that don't require a
// database round-trip (positive amount, distinct wallets, non-empty key).
// Wallet existence and balance are checked later, under lock, in the
// repository layer, since those checks are inherently race-prone otherwise.
func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrInvalidRequest
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	return nil
}
