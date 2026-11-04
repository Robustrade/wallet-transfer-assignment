package domain

import "errors"

var (
	ErrNotImplemented      = errors.New("not implemented")
	ErrInvalidTransfer     = errors.New("invalid transfer")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with different transfer details")
)
