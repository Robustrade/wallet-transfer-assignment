package domain

import "errors"

// Custom error types for better error handling and testing
var (
	ErrWalletNotFound            = errors.New("wallet not found")
	ErrSourceWalletNotFound      = errors.New("source wallet not found")
	ErrDestinationWalletNotFound = errors.New("destination wallet not found")
	ErrInsufficientBalance       = errors.New("insufficient balance")
	ErrSameWallet                = errors.New("cannot transfer to the same wallet")
	ErrInvalidAmount             = errors.New("transfer amount must be greater than 0")
	ErrInvalidIdempotencyKey     = errors.New("idempotency key is required and must be non-empty")
	ErrInvalidWalletID           = errors.New("wallet ID must be a valid UUID")
	ErrInvalidAmountFormat       = errors.New("amount must be a valid numeric string")
	ErrTransferNotFound          = errors.New("transfer not found")
	ErrFailedTransaction         = errors.New("transaction failed")
	ErrDuplicateIdempotencyKey   = errors.New("duplicate idempotency key - concurrent processing")
)
