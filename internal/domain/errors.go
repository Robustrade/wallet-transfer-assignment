package domain

import "errors"

var (
	ErrInvalidRequest         = errors.New("invalid request")
	ErrInvalidWalletID        = errors.New("invalid wallet id")
	ErrInvalidAmount          = errors.New("amount must be positive")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrNegativeBalance        = errors.New("balance cannot be negative")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrWalletExists           = errors.New("wallet already exists")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrIdempotencyKeyInUse    = errors.New("request with this idempotency key is already being processed")
	ErrIdempotencyKeyConflict = errors.New("idempotency key reused with a different request payload")
)
