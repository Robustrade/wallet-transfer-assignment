package domain

import "time"

// TransferStatus represents the current state of a transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a wallet-to-wallet money transfer.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	ErrorReason    *string        `json:"errorReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// CreateTransferRequest represents the payload for creating a new transfer.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate checks that the transfer request is well-formed.
func (r *CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return NewInvalidInputError("idempotencyKey is required")
	}
	if r.FromWalletID == "" {
		return NewInvalidInputError("fromWalletId is required")
	}
	if r.ToWalletID == "" {
		return NewInvalidInputError("toWalletId is required")
	}
	if r.Amount <= 0 {
		return NewInvalidInputError("amount must be a positive integer")
	}
	if r.FromWalletID == r.ToWalletID {
		return NewInvalidInputError("fromWalletId and toWalletId must be different")
	}
	return nil
}

// CanTransitionTo checks whether the transfer can move to the target state.
// Valid transitions: PENDING → PROCESSED, PENDING → FAILED.
// PROCESSED and FAILED are terminal states.
func (t *Transfer) CanTransitionTo(target TransferStatus) bool {
	switch t.Status {
	case TransferStatusPending:
		return target == TransferStatusProcessed || target == TransferStatusFailed
	default:
		return false
	}
}
