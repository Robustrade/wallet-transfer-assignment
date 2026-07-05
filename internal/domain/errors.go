package domain

import "errors"

var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrInsufficientBalance    = errors.New("insufficient balance")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyKeyConflict = errors.New("idempotency key reused with a different request payload")
	ErrTransferNotFound       = errors.New("transfer not found")
)

var ErrInvalidTransferTransition = errors.New("invalid transfer state transition")

var ErrIdempotencyInProgress = errors.New("a request with this idempotency key is already being processed")
