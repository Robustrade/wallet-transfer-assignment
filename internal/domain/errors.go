package domain

import "errors"

var (
	ErrInvalidAmount         = errors.New("amount must be a positive integer")
	ErrSameWallet            = errors.New("source and destination wallets must differ")
	ErrMissingIdempotencyKey = errors.New("idempotencyKey is required")
	ErrMissingWalletID       = errors.New("wallet id is required")
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrWalletExists          = errors.New("wallet already exists")
	ErrTransferNotFound      = errors.New("transfer not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with a different request")
	ErrIllegalTransition     = errors.New("illegal transfer status transition")
	ErrNegativeBalance       = errors.New("wallet balance cannot be negative")
)
