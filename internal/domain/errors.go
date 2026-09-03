package domain

import "errors"

var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrWalletExists        = errors.New("wallet already exists")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrInvalidAmount       = errors.New("amount must be a positive integer")
	ErrSameWallet          = errors.New("source and destination wallet must differ")
	ErrMissingField        = errors.New("required field is missing")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrDuplicateKey        = errors.New("idempotency key already exists")
)
