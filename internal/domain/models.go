package domain

import "time"

// TransferStatus represents the lifecycle state of a transfer.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// EntryType represents the direction of a ledger entry.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// Wallet holds a balance and is the primary actor in a transfer.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"` // stored in smallest currency unit (e.g. cents)
	CreatedAt time.Time `json:"createdAt"`
}

// Transfer represents a request to move funds between two wallets.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	FailureReason  *string        `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// LedgerEntry is one side of a double-entry bookkeeping record for a transfer.
type LedgerEntry struct {
	ID         int64     `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	Type       EntryType `json:"type"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}
