package models

import (
	"errors"
	"time"
)

// TransferStatus represents the state of a transfer
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a wallet-to-wallet transfer
type Transfer struct {
	ID           string
	FromWalletID string
	ToWalletID   string
	Amount       float64
	Status       TransferStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate validates transfer data
func (t *Transfer) Validate() error {
	if t.ID == "" {
		return errors.New("transfer ID cannot be empty")
	}
	if t.FromWalletID == "" {
		return errors.New("from wallet ID cannot be empty")
	}
	if t.ToWalletID == "" {
		return errors.New("to wallet ID cannot be empty")
	}
	if t.FromWalletID == t.ToWalletID {
		return errors.New("cannot transfer to the same wallet")
	}
	if t.Amount <= 0 {
		return errors.New("transfer amount must be positive")
	}
	if !t.IsValidStatus() {
		return errors.New("invalid transfer status")
	}
	return nil
}

// IsValidStatus checks if the transfer status is valid
func (t *Transfer) IsValidStatus() bool {
	switch t.Status {
	case TransferStatusPending, TransferStatusProcessed, TransferStatusFailed:
		return true
	default:
		return false
	}
}

// MarkAsProcessed updates the transfer status to PROCESSED
func (t *Transfer) MarkAsProcessed() error {
	if t.Status != TransferStatusPending {
		return errors.New("can only mark pending transfers as processed")
	}
	t.Status = TransferStatusProcessed
	t.UpdatedAt = time.Now()
	return nil
}

// MarkAsFailed updates the transfer status to FAILED
func (t *Transfer) MarkAsFailed() error {
	if t.Status != TransferStatusPending {
		return errors.New("can only mark pending transfers as failed")
	}
	t.Status = TransferStatusFailed
	t.UpdatedAt = time.Now()
	return nil
}

// IsPending checks if transfer is in pending state
func (t *Transfer) IsPending() bool {
	return t.Status == TransferStatusPending
}

// IsProcessed checks if transfer is completed
func (t *Transfer) IsProcessed() bool {
	return t.Status == TransferStatusProcessed
}

// IsFailed checks if transfer has failed
func (t *Transfer) IsFailed() bool {
	return t.Status == TransferStatusFailed
}
