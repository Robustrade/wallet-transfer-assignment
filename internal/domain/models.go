package domain

import (
	"time"
)

type Direction string

const (
	DirectionDebit  Direction = "DEBIT"
	DirectionCredit Direction = "CREDIT"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusCompleted TransferStatus = "COMPLETED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusResolved   IdempotencyStatus = "RESOLVED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// Wallet represents a customer wallet balance container
type Wallet struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Currency  string    `json:"currency"`
	Balance   int64     `json:"balance"` // Minor units (e.g., cents). Never use floats.
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Transfer records a financial movement between two wallets
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"` // Minor units (> 0)
	Currency       string         `json:"currency"`
	Status         TransferStatus `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// LedgerEntry is an immutable credit or debit record tied to a transfer
type LedgerEntry struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	Amount     int64     `json:"amount"` // Minor units (> 0)
	Direction  Direction `json:"direction"`
	CreatedAt  time.Time `json:"createdAt"`
}

// IdempotencyRecord maintains request lease locks and cached responses
type IdempotencyRecord struct {
	Key          string            `json:"key"`
	RequestHash  string            `json:"requestHash"`
	Status       IdempotencyStatus `json:"status"`
	ResponseCode int               `json:"responseCode,omitempty"`
	ResponseBody string            `json:"responseBody,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	LockedUntil  time.Time         `json:"lockedUntil"`
}
