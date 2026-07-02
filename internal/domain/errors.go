package domain

import "errors"

var (
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrInvalidAmount       = errors.New("amount must be positive")
	ErrSameWallet          = errors.New("from and to wallet must differ")
	ErrEmptyIdempotency    = errors.New("idempotency key is required")
	ErrEmptyWalletID       = errors.New("wallet id is required")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
	ErrInvalidRequest      = errors.New("invalid request body")
)
