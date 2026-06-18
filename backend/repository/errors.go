package repository

import "errors"

var (
	ErrInvalidAmount      = errors.New("amount must be positive")
	ErrSameWallet         = errors.New("source and destination wallet cannot be same")
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrMissingIdempotency = errors.New("missing idempotency key")
)
