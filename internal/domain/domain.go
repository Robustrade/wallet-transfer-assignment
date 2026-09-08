// Package domain contains the core entities, value objects, and business
// rules for the wallet transfer system. This package has zero dependencies
// on transport (HTTP) or persistence (SQL) concerns — it is pure Go.
package domain

import (
	"errors"
	"time"
)

// TransferStatus represents the state of a transfer in its lifecycle.
type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

// EntryType identifies whether a ledger entry is a debit or credit leg.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// Sentinel domain errors. Repository/service layers wrap these with context
// but callers (including HTTP handlers) can use errors.Is against them.
var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrWalletIdEmpty          = errors.New("wallet id is empty")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrInvalidAmount          = errors.New("amount must be a positive integer number of minor units")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrIdempotencyKeyEmpty    = errors.New("idempotencyKey is required")
	ErrIdempotencyConflict    = errors.New("idempotencyKey was already used with a different request payload")
	ErrIdempotencyInFlight    = errors.New("a request with this idempotencyKey is currently being processed")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)

// Wallet is the aggregate holding a balance. Balances are stored as integer
// minor units (e.g. cents) to avoid floating point rounding issues, which is
// standard practice for financial systems.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Transfer is the record of a wallet-to-wallet money movement.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LedgerEntry is one leg of a double-entry bookkeeping record. Every
// Transfer produces exactly two LedgerEntry rows: one DEBIT on the source
// wallet and one CREDIT on the destination wallet, both for the same amount.
type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// IdempotencyRecordStatus tracks the lifecycle of a claimed idempotency key.
type IdempotencyRecordStatus string

const (
	IdempotencyInProgress IdempotencyRecordStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyRecordStatus = "COMPLETED"
)

// IdempotencyRecord stores the outcome of a request keyed by client-supplied
// idempotencyKey, so retries and duplicate deliveries can be answered
// without re-running business logic or re-mutating balances.
type IdempotencyRecord struct {
	Key                string
	RequestFingerprint string
	Status             IdempotencyRecordStatus
	TransferID         string
	ResponseStatus     int
	ResponseBody       []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NewTransferRequest is the validated input to start a transfer. It is the
// domain-level equivalent of the HTTP request body — free of any JSON tags
// or transport concerns.
type NewTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate applies the business rules that must hold before any persistence
// or locking is attempted. It intentionally does NOT check wallet existence
// or balance — those require a database round trip and are the service
// layer's job.
func (r NewTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrIdempotencyKeyEmpty
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return ErrWalletIdEmpty
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

// CanTransitionTo enforces the transfer state machine:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// PROCESSED and FAILED are terminal states. This guards against accidental
// double-processing (e.g. a retried worker trying to re-apply a completed
// transfer) at the domain layer, independent of any database constraint.
func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	switch s {
	case TransferPending:
		return next == TransferProcessed || next == TransferFailed
	case TransferProcessed, TransferFailed:
		return false // terminal
	default:
		return false
	}
}

// BuildLedgerEntries constructs the two balanced ledger legs for a
// processed transfer. Kept in domain so the invariant ("every transfer
// produces exactly one DEBIT and one CREDIT of equal amount") lives in one
// place and is unit-testable without a database.
func BuildLedgerEntries(transferID, fromWalletID, toWalletID string, amount int64, debitID, creditID string) []LedgerEntry {
	return []LedgerEntry{
		{ID: debitID, TransferID: transferID, WalletID: fromWalletID, Type: EntryDebit, Amount: amount},
		{ID: creditID, TransferID: transferID, WalletID: toWalletID, Type: EntryCredit, Amount: amount},
	}
}
