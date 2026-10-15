package domain

import "errors"

// Sentinel errors returned by the service layer. Handlers map these to
// appropriate HTTP status codes without needing to know business details.
var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrMissingIdempotencyKey  = errors.New("idempotencyKey is required")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
