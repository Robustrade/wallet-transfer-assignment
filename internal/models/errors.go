package models

import "errors"

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrSameWallet        = errors.New("cannot transfer to the same wallet")
	ErrInvalidAmount     = errors.New("amount must be positive and within limit")
	ErrKeyMismatch       = errors.New("idempotency key reused with different parameters") // -> 409
	ErrInProgress        = errors.New("request already in progress")                      // -> 409/425
)
