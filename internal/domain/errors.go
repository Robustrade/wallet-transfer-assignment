package domain

import "errors"

var (
	ErrSameWalletTransfer    = errors.New("same wallet transfer not allowed")
	ErrInsufficientBalance   = errors.New("insufficient balance")
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrTransferNotFound = errors.New("transfer not found")
)
