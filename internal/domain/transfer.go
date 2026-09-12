package domain

import (
	"time"
)

// TransferStatus represents the current processing state of a transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// IsValid reports whether the status is a recognized transfer state.
func (s TransferStatus) IsValid() bool {
	switch s {
	case TransferStatusPending, TransferStatusProcessed, TransferStatusFailed:
		return true
	default:
		return false
	}
}

// Transfer represents a money transfer between two distinct wallets.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"` // Stored in minor currency units (e.g. cents)
	Status         TransferStatus `json:"status"`
	FailureReason  *string        `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// Validate ensures transfer inputs satisfy business invariants.
func (t *Transfer) Validate() error {
	if t.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if len(t.IdempotencyKey) > 128 {
		return ErrInvalidIdempotencyKey
	}
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return ErrMissingWalletID
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWalletTransfer
	}
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

// MarkProcessed transitions transfer state to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferStatusPending {
		return ErrInvalidStateTransition
	}
	t.Status = TransferStatusProcessed
	t.FailureReason = nil
	t.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkFailed transitions transfer state to FAILED with a recorded reason.
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != TransferStatusPending {
		return ErrInvalidStateTransition
	}
	t.Status = TransferStatusFailed
	t.FailureReason = &reason
	t.UpdatedAt = time.Now().UTC()
	return nil
}
