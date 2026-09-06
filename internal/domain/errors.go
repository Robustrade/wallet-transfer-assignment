package domain

import "errors"

var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrInvalidAmount          = errors.New("amount must be positive")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrIdempotencyKeyRequired = errors.New("idempotencyKey is required")
	ErrIdempotencyKeyConflict = errors.New("idempotencyKey already used with a different request")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
