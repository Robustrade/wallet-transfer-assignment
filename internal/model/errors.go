package model

import "errors"

var (
	// ErrInvalidAmount is returned when transfer amount is <= 0.
	ErrInvalidAmount = errors.New("transfer amount must be strictly positive")

	// ErrSelfTransfer is returned when source and destination wallets are identical.
	ErrSelfTransfer = errors.New("cannot transfer funds to the same wallet")

	// ErrWalletNotFound is returned when a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrInsufficientFunds is returned when the source wallet does not have enough balance.
	ErrInsufficientFunds = errors.New("insufficient funds in source wallet")

	// ErrIdempotencyConflict is returned when an idempotency key is reused with differing request payload.
	ErrIdempotencyConflict = errors.New("idempotency key reused with conflicting payload")

	// ErrIdempotencyInProgress is returned when a previous request with the same key is currently being processed.
	ErrIdempotencyInProgress = errors.New("request with this idempotency key is currently processing")

	// ErrBalanceOverflow is returned when crediting a wallet would overflow int64 maximum value.
	ErrBalanceOverflow = errors.New("wallet balance overflow")

	// ErrTransferNotFound is returned when querying for a transfer ID that does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInvalidIdempotencyKey is returned when the provided idempotency key is empty or invalid.
	ErrInvalidIdempotencyKey = errors.New("idempotency key must not be empty")

	// ErrCurrencyMismatch is returned when currencies between wallets do not match.
	ErrCurrencyMismatch = errors.New("wallet currency mismatch")
)
