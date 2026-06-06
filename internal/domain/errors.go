package domain

import "errors"

// Sentinel errors used across the service boundaries.
// Callers compare against these using errors.Is.
var (
	// ErrWalletNotFound is returned when a requested wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrTransferNotFound is returned when a requested transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInsufficientFunds is returned when the source wallet cannot cover the
	// requested transfer amount.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrInvalidAmount is returned when the transfer amount is not positive.
	ErrInvalidAmount = errors.New("amount must be greater than zero")

	// ErrSameWallet is returned when source and destination wallets are identical.
	ErrSameWallet = errors.New("source and destination wallets must differ")

	// ErrInvalidTransition is returned when a state transition is not allowed by
	// the transfer state machine.
	ErrInvalidTransition = errors.New("invalid transfer state transition")

	// ErrDuplicateIdempotencyKey is returned when an idempotency key is reused
	// with different request parameters (different wallets or amount).
	ErrDuplicateIdempotencyKey = errors.New("idempotency key already used with different parameters")

	// ErrIdempotencyKeyNotFound is returned by the idempotency repository when
	// no record exists for the given key.
	ErrIdempotencyKeyNotFound = errors.New("idempotency key not found")
)
