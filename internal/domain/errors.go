package domain

import "errors"

// Domain errors describe business rule violations independent of transport or
// persistence concerns. Higher layers map these to HTTP status codes.
var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrInvalidAmount       = errors.New("amount must be positive")
	ErrSameWallet          = errors.New("source and destination wallets must differ")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	ErrInvalidTransition   = errors.New("invalid transfer state transition")
)
