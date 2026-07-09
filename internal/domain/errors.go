package domain

import "errors"

var (
	ErrAmountMustBePositive      = errors.New("amount must be positive")
	ErrIdempotencyKeyRequired    = errors.New("idempotency key is required")
	ErrWalletIDRequired          = errors.New("wallet ids are required")
	ErrSameWalletTransfer        = errors.New("source and destination wallets must differ")
	ErrIdempotencyConflict       = errors.New("idempotency key was already used with a different request")
	ErrWalletNotFound            = errors.New("wallet not found")
	ErrInsufficientFunds         = errors.New("insufficient funds")
	ErrTransferNotFound          = errors.New("transfer not found")
	ErrInvalidTransferTransition = errors.New("invalid transfer state transition")
)
