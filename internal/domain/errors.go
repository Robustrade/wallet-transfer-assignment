package domain

import "errors"

var (
	// ErrWalletNotFound is returned when a wallet ID does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrInsufficientFunds is returned when the source wallet cannot cover the transfer amount.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrSameWallet is returned when source and destination wallet IDs are equal.
	ErrSameWallet = errors.New("source and destination wallets must be different")

	// ErrInvalidAmount is returned when the requested transfer amount is not positive.
	ErrInvalidAmount = errors.New("amount must be greater than zero")

	// ErrMissingIdempotencyKey is returned when no idempotency key is provided.
	ErrMissingIdempotencyKey = errors.New("idempotencyKey is required")

	// ErrTransferNotFound is returned when a transfer record cannot be located.
	ErrTransferNotFound = errors.New("transfer not found")
)
