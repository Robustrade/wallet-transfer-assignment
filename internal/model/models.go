package model

import "time"

type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TransferStatus string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type LedgerEntryType string

const (
	Debit  LedgerEntryType = "DEBIT"
	Credit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64           `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}
