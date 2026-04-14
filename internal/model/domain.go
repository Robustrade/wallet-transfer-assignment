package model

import (
	"errors"
	"time"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transfer struct {
	ID                 string         `json:"id"`
	IdempotencyKey     string         `json:"idempotency_key"`
	FromWalletID       string         `json:"from_wallet_id"`
	ToWalletID         string         `json:"to_wallet_id"`
	Amount             int64          `json:"amount"`
	Status             TransferStatus `json:"status"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	IsIdempotentReplay bool           `json:"-"`
}

// Validate executes core domain validation rules to ensure safe state transitions.
func (t *Transfer) Validate() error {
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return errors.New("from and to wallet IDs are required")
	}
	if t.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	if t.FromWalletID == t.ToWalletID {
		return errors.New("cannot transfer to the same wallet")
	}
	if t.IdempotencyKey == "" {
		return errors.New("idempotency key is required")
	}
	return nil
}

type LedgerEntryType string

const (
	EntryTypeDebit  LedgerEntryType = "DEBIT"
	EntryTypeCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64           `json:"id"`
	TransferID string          `json:"transfer_id"`
	WalletID   string          `json:"wallet_id"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"created_at"`
}
