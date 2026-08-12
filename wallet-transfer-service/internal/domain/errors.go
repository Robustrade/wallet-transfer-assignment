package domain

import "errors"

var (
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrInvalidTransfer       = errors.New("invalid transfer")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with different request")
	ErrIdempotencyInProgress = errors.New("idempotency request is already in progress")
)
