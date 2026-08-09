package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// TransferStatus represents the lifecycle state of a wallet transfer.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// LedgerEntryType represents the accounting entry direction.
type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

// IdempotencyStatus represents the state of an idempotency request.
type IdempotencyStatus string

const (
	IdempotencyStatusInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed     IdempotencyStatus = "FAILED"
)

// Wallet represents an account balance entity.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"` // Stored in minor units (e.g., cents)
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Transfer represents a wallet-to-wallet funds transfer transaction.
type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"` // Stored in minor units (e.g., cents)
	Status         TransferStatus `json:"status"`
	FailureReason  string         `json:"failureReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// LedgerEntry represents a single double-entry ledger debit or credit record.
type LedgerEntry struct {
	ID         string          `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"` // Stored in minor units (e.g., cents)
	CreatedAt  time.Time       `json:"createdAt"`
}

// IdempotencyRecord tracks request deduplication state and response payload.
type IdempotencyRecord struct {
	IdempotencyKey string            `json:"idempotencyKey"`
	RequestHash    string            `json:"requestHash"`
	Status         IdempotencyStatus `json:"status"`
	ResponseCode   int               `json:"responseCode"`
	ResponseBody   string            `json:"responseBody"`
	TransferID     string            `json:"transferId,omitempty"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

// ComputeRequestHash generates a deterministic SHA-256 hash of transfer request inputs.
func ComputeRequestHash(fromWalletID, toWalletID string, amount int64) string {
	payload := fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}
