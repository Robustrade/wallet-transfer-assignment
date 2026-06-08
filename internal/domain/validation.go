package domain

import "github.com/google/uuid"

// CreateTransferRequest is the validated, parsed input for the transfer service.
type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         Amount
}

// Validate performs domain-level validation on the request.
func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if !r.Amount.IsPositive() {
		return ErrInvalidAmount
	}
	return nil
}
