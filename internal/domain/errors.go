package domain

import "errors"

var (
	ErrInvalidIdempotencyKey = errors.New("idempotency key is required and must not exceed 255 characters")
	ErrInvalidWalletID       = errors.New("wallet IDs are required and must not exceed 255 characters")
	ErrSameWallet            = errors.New("source and destination wallets must differ")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrInvalidTransition     = errors.New("invalid transfer state transition")
	ErrIdempotencyConflict   = errors.New("idempotency key was already used with a different request")
)
