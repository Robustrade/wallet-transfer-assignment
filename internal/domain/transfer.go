package domain

import "time"

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}
