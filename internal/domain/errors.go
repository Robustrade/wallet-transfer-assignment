package domain

import "errors"

var (
	ErrInsufficientFunds      = errors.New("insufficient funds for transfer")
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrCurrencyMismatch       = errors.New("currency mismatch between wallets or transfer request")
	ErrSelfTransfer           = errors.New("cannot transfer funds to the same wallet")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrConcurrentRequest      = errors.New("request currently in progress")
	ErrIdempotencyKeyConflict = errors.New("idempotency key reused with differing payload parameters")
	ErrIdempotencyKeyRequired = errors.New("idempotencyKey is required")
)
