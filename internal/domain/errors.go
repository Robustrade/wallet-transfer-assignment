package domain

import "errors"

var (
	ErrNotFound            = errors.New("not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrInvalidTransfer     = errors.New("invalid transfer")
	ErrInvalidState        = errors.New("invalid state transition")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrSameWallet          = errors.New("source and target wallet must differ")
)
