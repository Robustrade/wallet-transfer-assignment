package domain

import "errors"

// Domain-level sentinel errors. Outer layers map these to transport concerns
// (HTTP status codes) and persistence never leaks driver errors past the
// repository boundary.
var (
	// ErrValidation indicates a malformed or semantically invalid command.
	ErrValidation = errors.New("validation error")

	// ErrWalletNotFound is returned when a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrTransferNotFound is returned when a referenced transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInsufficientFunds indicates the source wallet cannot cover the transfer.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrInvalidStateTransition indicates an illegal transfer state change.
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")

	// ErrCurrencyMismatch indicates the wallets/amount currencies do not agree.
	ErrCurrencyMismatch = errors.New("currency mismatch")

	// ErrIdempotencyKeyConflict indicates the idempotency key was reused with a
	// different request payload.
	ErrIdempotencyKeyConflict = errors.New("idempotency key reused with a different payload")

	// ErrDuplicateIdempotencyKey is a low-level signal from the repository that a
	// key already exists. The service turns this into a safe replay.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
)
