package domain

import "errors"

// Sentinel errors for the domain layer.
var (
	ErrMissingIdempotencyKey = errors.New("idempotencyKey is required")
	ErrMissingFromWallet     = errors.New("fromWalletId is required")
	ErrMissingToWallet       = errors.New("toWalletId is required")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrSelfTransfer          = errors.New("cannot transfer to the same wallet")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrTransferNotFound      = errors.New("transfer not found")
)
