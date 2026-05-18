package domain

import "errors"

var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransactionNotFound = errors.New("transaction not found")

	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrSameWallet         = errors.New("source and destination wallet must differ")
	ErrInvalidAmount      = errors.New("amount must be greater than zero")
	ErrCurrencyMismatch   = errors.New("wallet currencies do not match")
	ErrNegativeBalance    = errors.New("operation would result in negative balance")

	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
	ErrIdempotencyConflict     = errors.New("idempotency key conflict: request parameters do not match original transaction")
)
