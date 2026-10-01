package domain

import (
	"errors"
	"fmt"
	"time"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

const (
	FailureInsufficientFunds = "INSUFFICIENT_FUNDS"
	FailureBalanceOverflow   = "BALANCE_OVERFLOW"
)

var (
	ErrValidation        = errors.New("validation error")
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletExists      = errors.New("wallet already exists")
)

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
}

type LedgerEntry struct {
	WalletID string
	Type     EntryType
	Amount   int64
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	Ledger         []LedgerEntry
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

func (r CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotencyKey is required", ErrValidation)
	}
	if r.FromWalletID == "" {
		return fmt.Errorf("%w: fromWalletId is required", ErrValidation)
	}
	if r.ToWalletID == "" {
		return fmt.Errorf("%w: toWalletId is required", ErrValidation)
	}
	if r.FromWalletID == r.ToWalletID {
		return fmt.Errorf("%w: fromWalletId and toWalletId must differ", ErrValidation)
	}
	if r.Amount <= 0 {
		return fmt.Errorf("%w: amount must be a positive integer", ErrValidation)
	}
	return nil
}

type CreateWalletRequest struct {
	ID             string
	InitialBalance int64
}

func (r CreateWalletRequest) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("%w: id is required", ErrValidation)
	}
	if r.InitialBalance < 0 {
		return fmt.Errorf("%w: initialBalance cannot be negative", ErrValidation)
	}
	return nil
}
