package domain

import "errors"

var (
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrDuplicateRequest   = errors.New("duplicate request")
	ErrInvalidIdempotency = errors.New("invalid idempotency key or payload mismatch")
	ErrWalletNotFound     = errors.New("wallet not found")
)
