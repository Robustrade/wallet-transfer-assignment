package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Transfer struct {
	ID               uuid.UUID
	IdempotencyKey   string
	FromWalletID     string
	ToWalletID       string
	Amount           int64
	Status           TransferStatus
	FailureReason    *string
	ResponseSnapshot json.RawMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResult struct {
	TransferID     string         `json:"transferId"`
	Status         TransferStatus `json:"status"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	LedgerEntryIDs []int64        `json:"ledgerEntryIds,omitempty"`
	FailureReason  string         `json:"failureReason,omitempty"`
	IsReplay       bool           `json:"-"`
}

func (r TransferResult) Snapshot() (json.RawMessage, error) {
	return json.Marshal(r)
}

func TransferResultFromSnapshot(data json.RawMessage) (TransferResult, error) {
	var result TransferResult
	if err := json.Unmarshal(data, &result); err != nil {
		return TransferResult{}, err
	}
	result.IsReplay = true
	return result, nil
}

func CanTransition(from, to TransferStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusPending:
		return to == StatusProcessed || to == StatusFailed
	default:
		return false
	}
}
