// Package domain holds wallet, transfer, and ledger rules.
// It does not import HTTP or database packages.
package domain

import "errors"

var (
	// ErrNotFound means the wallet or transfer does not exist.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists means a wallet id is already stored.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalidInput means the caller sent a request the service will not attempt.
	ErrInvalidInput = errors.New("invalid input")
	// ErrIdempotencyConflict means the key was stored for a different payload.
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	// ErrBusy means the idempotency key could not be claimed after a short retry.
	ErrBusy = errors.New("idempotency key is busy")
	// ErrInsufficientFunds means a locked debit found too little balance.
	// The service treats this as a transaction failure and rolls back.
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// InputError is a validation failure. The message is safe to return to the caller.
type InputError struct {
	Message string
}

func (e *InputError) Error() string { return e.Message }

func (e *InputError) Unwrap() error { return ErrInvalidInput }

// Status is the transfer state.
type Status string

const (
	StatusPending   Status = "PENDING"
	StatusProcessed Status = "PROCESSED"
	StatusFailed    Status = "FAILED"
)

const (
	// FailureInsufficientFunds is stored when the source balance is too small.
	FailureInsufficientFunds = "insufficient_funds"
	// FailureBalanceOverflow is stored when the credit would exceed int64.
	FailureBalanceOverflow = "balance_overflow"
)

// EntryType is one side of a double-entry pair.
type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

// Wallet is an account with a stored balance.
type Wallet struct {
	ID             string
	Balance        int64
	InitialBalance int64
}

// Transfer is one wallet-to-wallet movement.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         Status
	FailureReason  string
}

// LedgerEntry is one debit or one credit.
type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     int64
}

// CanTransition reports whether a transfer may move from one status to another.
// Only PENDING may change, and only to a terminal status.
func CanTransition(from, to Status) bool {
	if from != StatusPending {
		return false
	}
	return to == StatusProcessed || to == StatusFailed
}
