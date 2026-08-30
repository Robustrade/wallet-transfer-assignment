package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

const (
	StatusPending   = "PENDING"
	StatusProcessed = "PROCESSED"
	StatusFailed    = "FAILED"

	EntryDebit  = "DEBIT"
	EntryCredit = "CREDIT"

	IdempotencyInProgress = "IN_PROGRESS"
	IdempotencyCompleted  = "COMPLETED"

	FailureInsufficientFunds = "INSUFFICIENT_FUNDS"
)

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrWalletExists        = errors.New("wallet already exists")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	ErrIdempotencyRetry    = errors.New("idempotency claim raced; retry")
	ErrInvalidStateChange  = errors.New("invalid transfer state transition")
)

type Wallet struct {
	ID      string
	Balance int64
}

type Transfer struct {
	ID            string
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        string
	FailureReason string
}

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       string
	Amount     int64
}

type CreateWalletInput struct {
	ID      string
	Balance int64
}

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

func (in CreateWalletInput) Validate() error {
	if in.Balance < 0 {
		return fmt.Errorf("%w: balance must be >= 0", ErrInvalidRequest)
	}
	return nil
}

func (in CreateTransferInput) Validate() error {
	if in.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotencyKey is required", ErrInvalidRequest)
	}
	if in.FromWalletID == "" || in.ToWalletID == "" {
		return fmt.Errorf("%w: fromWalletId and toWalletId are required", ErrInvalidRequest)
	}
	if in.FromWalletID == in.ToWalletID {
		return fmt.Errorf("%w: fromWalletId and toWalletId must differ", ErrInvalidRequest)
	}
	if in.Amount <= 0 {
		return fmt.Errorf("%w: amount must be > 0", ErrInvalidRequest)
	}
	return nil
}

func (in CreateTransferInput) RequestHash() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", in.FromWalletID, in.ToWalletID, in.Amount)))
	return hex.EncodeToString(sum[:])
}

func CanTransition(from, to string) error {
	if from != StatusPending {
		return ErrInvalidStateChange
	}
	if to != StatusProcessed && to != StatusFailed {
		return ErrInvalidStateChange
	}
	return nil
}
