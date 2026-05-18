package app

import "time"

type TransferState string

type LedgerEntryType string

const (
	StatePending   TransferState = "PENDING"
	StateProcessed TransferState = "PROCESSED"
	StateFailed    TransferState = "FAILED"

	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
}

type Transfer struct {
	ID             string        `json:"id"`
	FromWalletID   string        `json:"fromWalletId"`
	ToWalletID     string        `json:"toWalletId"`
	Amount         int64         `json:"amount"`
	State          TransferState `json:"state"`
	IdempotencyKey string        `json:"idempotencyKey"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type LedgerEntry struct {
	ID         int64           `json:"id"`
	WalletID   string          `json:"walletId"`
	TransferID string          `json:"transferId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"createdAt"`
}
