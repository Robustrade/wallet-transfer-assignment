package domain

import "errors"

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrDuplicateRequest  = errors.New("duplicate request")
	ErrInvalidAmount     = errors.New("transfer amount must be greater than zero")
	ErrSameWallet        = errors.New("source and destination wallets must be different")
	ErrConcurrentRequest = errors.New("request with this idempotency key is already being processed")
)
