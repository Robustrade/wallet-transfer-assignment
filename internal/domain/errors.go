package domain

import "errors"

var (
	ErrInvalidTransfer     = errors.New("invalid transfer")
	ErrInvalidWallet       = errors.New("invalid wallet")
	ErrWalletAlreadyExists = errors.New("wallet already exists")
	ErrWalletInactive      = errors.New("wallet is inactive")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with different transfer details")
)
