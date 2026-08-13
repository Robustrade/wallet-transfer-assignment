package domain

import "time"

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

// CanTransitionTo enforces the transfer state machine: PENDING is the only
// non-terminal state, and it may only move forward to PROCESSED or FAILED.
func (s TransferStatus) CanTransitionTo(target TransferStatus) bool {
	if s != TransferPending {
		return false
	}
	return target == TransferProcessed || target == TransferFailed
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPendingTransfer constructs a transfer in its initial state. Validation
// of the fields (positive amount, distinct wallets) is the caller's job —
// this constructor just fixes the invariant that every transfer starts PENDING.
func NewPendingTransfer(id, idempotencyKey, fromWalletID, toWalletID string, amount int64) *Transfer {
	return &Transfer{
		ID:             id,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         amount,
		Status:         TransferPending,
	}
}
