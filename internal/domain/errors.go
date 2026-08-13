package domain

import "errors"

var (
	ErrWalletNotFound            = errors.New("wallet not found")
	ErrWalletAlreadyExists       = errors.New("wallet already exists")
	ErrInsufficientFunds         = errors.New("insufficient funds")
	ErrSameWallet                = errors.New("source and destination wallet must differ")
	ErrCurrencyMismatch          = errors.New("source and destination wallet currencies must match")
	ErrInvalidAmount             = errors.New("amount must be positive")
	ErrMissingIdempotencyKey     = errors.New("idempotency key is required")
	ErrIdempotencyKeyReused      = errors.New("idempotency key reused with a different payload")
	ErrRequestInProgress         = errors.New("request with this idempotency key is already in progress")
	ErrTransferNotFound          = errors.New("transfer not found")
	ErrInvalidTransition         = errors.New("invalid transfer state transition")
	ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")
	// ErrCommitOutcomeUnknown means a transaction's COMMIT was sent but its
	// result could not be confirmed (e.g. the connection dropped waiting for
	// the acknowledgement). The transaction may have actually landed on the
	// server despite the client-side error, so callers must not treat this
	// the same as a definite rollback/no-op.
	ErrCommitOutcomeUnknown = errors.New("transaction commit outcome unknown")
)
