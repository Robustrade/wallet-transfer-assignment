package domain

import "errors"

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrInvalidWalletID       = errors.New("wallet ID is required")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrSameWallet            = errors.New("source and destination wallets must be different")
	ErrInvalidIdempotencyKey = errors.New("idempotency key is required")
	ErrIdempotencyConflict   = errors.New("idempotency key already used with different request")
	ErrTransferNotFound      = errors.New("transfer not found")
)
