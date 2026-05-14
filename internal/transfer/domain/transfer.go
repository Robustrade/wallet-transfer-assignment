package domain

import (
	"fmt"
	"time"
)

// TransferStatus represents transfer state
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a transfer between wallets
type Transfer struct {
	ID           string
	FromWalletID string
	ToWalletID   string
	Amount       int64 // Amount in cents
	Status       TransferStatus
	ErrorReason  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewTransfer creates a new transfer
func NewTransfer(id string, fromWalletID string, toWalletID string, amount int64) (*Transfer, error) {
	if fromWalletID == toWalletID {
		return nil, fmt.Errorf("cannot transfer to the same wallet")
	}
	if amount <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	return &Transfer{
		ID:           id,
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       StatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}, nil
}

// MarkProcessed marks transfer as processed
func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return fmt.Errorf("can only mark pending transfers as processed")
	}
	t.Status = StatusProcessed
	t.UpdatedAt = time.Now()
	return nil
}

// MarkFailed marks transfer as failed
func (t *Transfer) MarkFailed(reason string) error {
	if t.Status != StatusPending {
		return fmt.Errorf("can only mark pending transfers as failed")
	}
	t.Status = StatusFailed
	t.ErrorReason = &reason
	t.UpdatedAt = time.Now()
	return nil
}

// IsProcessed checks if transfer is processed
func (t *Transfer) IsProcessed() bool {
	return t.Status == StatusProcessed
}

// IsFailed checks if transfer failed
func (t *Transfer) IsFailed() bool {
	return t.Status == StatusFailed
}

// IsPending checks if transfer is pending
func (t *Transfer) IsPending() bool {
	return t.Status == StatusPending
}
