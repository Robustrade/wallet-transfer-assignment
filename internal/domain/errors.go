package domain

import "errors"

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrSameWallet            = errors.New("from and to wallets must differ")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrMissingIdempotencyKey = errors.New("idempotencyKey is required")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with different payload")
	ErrInvariantViolation    = errors.New("invariant violation")
)
