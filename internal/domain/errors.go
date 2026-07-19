package domain

import "errors"

var (
	ErrInvalidAmount       = errors.New("invalid amount")
	ErrInvalidCurrency     = errors.New("invalid currency")
	ErrInvalidIdempotency  = errors.New("invalid idempotency key")
	ErrInvalidWalletID     = errors.New("invalid wallet id")
	ErrSameWallet          = errors.New("source and destination wallets must differ")
	ErrInvalidState        = errors.New("invalid transfer state")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrWalletNotActive     = errors.New("wallet not active")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different payload")
	ErrRequestInProgress   = errors.New("request in progress")
)
