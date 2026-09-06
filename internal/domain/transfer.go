package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// TransferStatus represents a state in the transfer state machine.
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// Transfer is the domain entity for a wallet-to-wallet transfer.
type Transfer struct {
	ID            uuid.UUID
	FromWalletID  uuid.UUID
	ToWalletID    uuid.UUID
	Amount        int64 // minor units (cents); see money.go for the exact conversion to/from NUMERIC
	Status        TransferStatus
	FailureReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EntryType is the double-entry ledger entry type.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// Domain errors. Handlers map these to HTTP status codes; they are never
// exposed to clients as raw database/internal errors.
var (
	ErrInvalidAmount       = errors.New("amount must be positive")
	ErrSameWallet          = errors.New("source and destination wallet must differ")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientFunds   = errors.New("insufficient wallet balance")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request parameters")
	ErrIdempotencyInFlight = errors.New("a request with this idempotency key is already being processed")
	ErrInvalidTransition   = errors.New("invalid transfer state transition")
	ErrInvalidRequest      = errors.New("invalid request")
)

// CanTransition reports whether moving from `from` to `to` is a legal
// state-machine transition. PROCESSED and FAILED are terminal states.
func CanTransition(from, to TransferStatus) bool {
	switch from {
	case StatusPending:
		return to == StatusProcessed || to == StatusFailed
	default:
		return false
	}
}
