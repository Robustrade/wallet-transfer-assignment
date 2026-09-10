package domain

import "errors"

// Domain errors used across the application layers.
var (
	// ErrInsufficientFunds is returned when a wallet does not have enough balance.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrWalletNotFound is returned when a wallet cannot be found.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrTransferNotFound is returned when a transfer cannot be found.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrDuplicateIdempotencyKey is returned when an idempotency key already exists.
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")

	// ErrInvalidStateTransition is returned when a state transition is not allowed.
	ErrInvalidStateTransition = errors.New("invalid state transition")

	// ErrSameWallet is returned when source and destination wallets are the same.
	ErrSameWallet = errors.New("source and destination wallets must be different")

	// ErrInvalidAmount is returned when the transfer amount is non-positive.
	ErrInvalidAmount = errors.New("amount must be positive")

	// ErrEmptyIdempotencyKey is returned when the idempotency key is empty.
	ErrEmptyIdempotencyKey = errors.New("idempotency key is required")

	// ErrEmptyWalletID is returned when a wallet ID is empty.
	ErrEmptyWalletID = errors.New("wallet ID is required")

	// ErrDuplicateWallet is returned when a wallet with the same ID already exists.
	ErrDuplicateWallet = errors.New("wallet already exists")
)
