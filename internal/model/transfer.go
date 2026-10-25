package model

import (
	"strings"
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// Transfer represents a wallet-to-wallet transfer domain entity.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"` // Strictly positive integer
	Status         TransferStatus `json:"status"`
	FailureReason  *string        `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// Validate ensures transfer inputs satisfy core business validation rules before transaction execution.
func (t *Transfer) Validate() error {
	if strings.TrimSpace(t.IdempotencyKey) == "" {
		return ErrInvalidIdempotencyKey
	}
	if strings.TrimSpace(t.FromWalletID) == "" || strings.TrimSpace(t.ToWalletID) == "" {
		return ErrWalletNotFound
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSelfTransfer
	}
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
