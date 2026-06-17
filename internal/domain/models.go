package domain

import (
	"errors"
	"time"
)

// TransferStatus is the lifecycle state of a transfer.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// LedgerEntryType is the direction of a ledger entry.
type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

// Domain sentinel errors. Upper layers map these to transport-level responses.
var (
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrInvalidAmount          = errors.New("amount must be positive")
	ErrSameWallet             = errors.New("source and destination wallets must differ")
	ErrMissingWallet          = errors.New("wallet id must not be empty")
)

// Wallet holds a stored balance in minor units (e.g. cents).
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Transfer represents a wallet-to-wallet movement of funds.
type Transfer struct {
	ID           string
	FromWalletID string
	ToWalletID   string
	Amount       int64
	Status       TransferStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// LedgerEntry is one half of a double-entry record.
type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}