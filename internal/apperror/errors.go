package apperror

import "errors"

var (
	// Validation & Business Rule errors
	ErrInvalidAmount         = errors.New("transfer amount must be strictly greater than zero")
	ErrInvalidInitialBalance = errors.New("initial balance cannot be negative")

	// Wallet & Transfer domain errors
	ErrWalletNotFound      = errors.New("one or both wallets not found")
	ErrInsufficientBalance = errors.New("insufficient balance in source wallet")
	ErrSelfTransfer        = errors.New("cannot transfer to the same wallet")
	ErrSourceDebitBlocked  = errors.New("source wallet is frozen for debits")
	ErrDestCreditBlocked   = errors.New("destination wallet is frozen for credits")
	ErrWalletFrozen        = errors.New("wallet is completely frozen")
	ErrWalletClosed        = errors.New("wallet is closed")
	ErrWalletAlreadyExists = errors.New("wallet already exists")
	ErrWalletIDRequired    = errors.New("wallet id is required")
	ErrBalanceOverflow     = errors.New("destination balance overflow limit reached")

	// Idempotency errors
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrRequestInProgress      = errors.New("request currently in progress, please retry shortly")
	ErrPayloadMismatch        = errors.New("idempotency key already used with different payload")
	ErrIdempotencyKeyExpired  = errors.New("idempotency key has expired; please generate a fresh key")
)
