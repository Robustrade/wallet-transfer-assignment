package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

type TransferStatus string
type LedgerEntryType string

const (
	TransferStatusPending   TransferStatus  = "PENDING"
	TransferStatusProcessed TransferStatus  = "PROCESSED"
	TransferStatusFailed    TransferStatus  = "FAILED"
	LedgerEntryTypeDebit    LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit   LedgerEntryType = "CREDIT"
)

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrSameWallet            = errors.New("source and destination wallets must be different")
	ErrInvalidAmount         = errors.New("transfer amount must be greater than zero")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with different request")
	ErrInvalidTransferState  = errors.New("invalid transfer state transition")
	ErrIdempotencyKeyExists  = errors.New("idempotency key already exists")
	ErrIdempotencyNotFound   = errors.New("idempotency record not found")
	ErrInvalidIdempotencyKey = errors.New("idempotency key is required")
)

type Wallet struct {
	ID        uuid.UUID
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Transfer struct {
	ID           uuid.UUID
	FromWalletID uuid.UUID
	ToWalletID   uuid.UUID
	Amount       int64
	Status       TransferStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	TransferID     *uuid.UUID
	ResponseCode   *int
	ResponseBody   []byte
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   uuid.UUID
	ToWalletID     uuid.UUID
	Amount         int64
}

type CreateTransferResult struct {
	TransferID uuid.UUID      `json:"transferId"`
	Status     TransferStatus `json:"status"`
	Replayed   bool           `json:"-"`
	Message    string         `json:"message,omitempty"`
}
