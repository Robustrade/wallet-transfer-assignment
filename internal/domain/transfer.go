package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidAmount       = errors.New("transfer amount must be strictly greater than zero")
	ErrSameWalletTransfer  = errors.New("cannot transfer funds to the same wallet")
	ErrEmptyIdempotencyKey = errors.New("idempotency key cannot be empty")
	ErrEmptyWalletID       = errors.New("wallet id cannot be empty")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

const (
	ErrReasonInsufficientFunds = "INSUFFICIENT_FUNDS"
)

type Transfer struct {
	ID             string         `json:"id"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletID   string         `json:"fromWalletId"`
	ToWalletID     string         `json:"toWalletId"`
	Amount         int64          `json:"amount"`
	Status         TransferStatus `json:"status"`
	ErrorReason    *string        `json:"errorReason,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type LedgerEntryType string

const (
	LedgerEntryDebit  LedgerEntryType = "DEBIT"
	LedgerEntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string          `json:"id"`
	TransferID string          `json:"transferId"`
	WalletID   string          `json:"walletId"`
	Type       LedgerEntryType `json:"type"`
	Amount     int64           `json:"amount"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r *CreateTransferRequest) Validate() error {
	r.IdempotencyKey = strings.TrimSpace(r.IdempotencyKey)
	r.FromWalletID = strings.TrimSpace(r.FromWalletID)
	r.ToWalletID = strings.TrimSpace(r.ToWalletID)

	if r.IdempotencyKey == "" {
		return ErrEmptyIdempotencyKey
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return ErrEmptyWalletID
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWalletTransfer
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
