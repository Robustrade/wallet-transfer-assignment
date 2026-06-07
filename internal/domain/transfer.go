package domain

import (
	"errors"
	"time"
)

// TransferStatus represents the state of a transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a wallet-to-wallet transfer.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  string         `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// Validate checks that the transfer request is valid.
func (t *Transfer) Validate() error {
	if t.IdempotencyKey == "" {
		return errors.New("idempotencyKey is required")
	}
	if t.FromWalletID == "" {
		return errors.New("fromWalletId is required")
	}
	if t.ToWalletID == "" {
		return errors.New("toWalletId is required")
	}
	if t.FromWalletID == t.ToWalletID {
		return errors.New("fromWalletId and toWalletId must be different")
	}
	if t.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	return nil
}

// CanTransitionTo checks if the state transition is valid.
func (t *Transfer) CanTransitionTo(newStatus TransferStatus) bool {
	switch t.Status {
	case TransferStatusPending:
		return newStatus == TransferStatusProcessed || newStatus == TransferStatusFailed
	default:
		// PROCESSED and FAILED are terminal states
		return false
	}
}

func (t *Transfer) MarkProcessed() error {
	if !t.CanTransitionTo(TransferStatusProcessed) {
		return errors.New("cannot transition to PROCESSED from " + string(t.Status))
	}
	t.Status = TransferStatusProcessed
	t.UpdatedAt = time.Now()
	return nil
}

// MarkFailed transitions the transfer to FAILED with a reason.
func (t *Transfer) MarkFailed(reason string) error {
	if !t.CanTransitionTo(TransferStatusFailed) {
		return errors.New("cannot transition to FAILED from " + string(t.Status))
	}
	t.Status = TransferStatusFailed
	t.FailureReason = reason
	t.UpdatedAt = time.Now()
	return nil
}
