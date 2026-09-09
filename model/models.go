package model

import (
	"encoding/json"
	"time"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

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
	TransferID string
	WalletID   string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type TransferRequest struct {
	IdempotencyKey string      `json:"idempotencyKey"`
	FromWalletID   string      `json:"fromWalletId"`
	ToWalletID     string      `json:"toWalletId"`
	Amount         json.Number `json:"amount"`
}

type TransferResponse struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         json.Number    `json:"amount"`
	Status         TransferStatus `json:"status"`
}
