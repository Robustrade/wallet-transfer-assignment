// Package domain holds entities, value rules, and the transfer state machine.
// It has no infrastructure dependencies.
package domain

import "errors"

// Validation errors: the request itself is invalid, so reject it before any work.
var (
	ErrEmptyIdempotencyKey   = errors.New("idempotency key is required")
	ErrIdempotencyKeyTooLong = errors.New("idempotency key is too long")
	ErrInvalidAmount         = errors.New("amount must be positive")
	ErrSameWallet            = errors.New("source and destination wallets must differ")
)

// Business failures: the request is valid but cannot succeed. These are saved as a
// FAILED transfer, so sending the same request again returns the same result.
var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrWalletInactive    = errors.New("wallet is inactive")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Lookup errors.
var ErrTransferNotFound = errors.New("transfer not found")
