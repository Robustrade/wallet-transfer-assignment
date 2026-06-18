package repository

import "errors"

// Persistence-level sentinel errors.
var (
	ErrWalletNotFound          = errors.New("wallet not found")
	ErrTransferNotFound        = errors.New("transfer not found")
	ErrDuplicateIdempotencyKey = errors.New("idempotency key already exists")
	ErrIdempotencyKeyNotFound  = errors.New("idempotency key not found")
)