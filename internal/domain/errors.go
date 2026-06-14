package domain

import "errors"

// Sentinel errors used across the service and repository layers.
// Handlers map these to HTTP status codes — no HTTP concepts leak in here.
var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrTransferNotFound  = errors.New("transfer not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrDuplicateKey      = errors.New("duplicate idempotency key")
)
