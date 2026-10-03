package domain

import "errors"

var (
	// ErrWalletNotFound is returned when a requested wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrSourceWalletNotFound indicates the sender wallet does not exist.
	ErrSourceWalletNotFound = errors.New("source wallet not found")

	// ErrDestinationWalletNotFound indicates the receiver wallet does not exist.
	ErrDestinationWalletNotFound = errors.New("destination wallet not found")

	// ErrInsufficientBalance indicates source wallet has insufficient funds.
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrInvalidAmount indicates the transfer amount is not positive.
	ErrInvalidAmount = errors.New("transfer amount must be strictly greater than zero")

	// ErrSameWalletTransfer indicates sender and receiver wallets are identical.
	ErrSameWalletTransfer = errors.New("source and destination wallets cannot be the same")

	// ErrWalletInactive indicates one or both wallets are frozen or inactive.
	ErrWalletInactive = errors.New("wallet is not active")

	// ErrMissingIdempotencyKey indicates idempotency key was omitted.
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")

	// ErrIdempotencyConflict indicates a concurrent request with the same key is in-flight.
	ErrIdempotencyConflict = errors.New("a transfer with this idempotency key is currently processing")

	// ErrIdempotencyPayloadMismatch indicates key reuse with different parameters.
	ErrIdempotencyPayloadMismatch = errors.New("idempotency key was previously used with different parameters")

	// ErrTransferNotFound is returned when a transfer lookup fails.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrCurrencyMismatch indicates wallet currencies do not match each other or the transfer request.
	ErrCurrencyMismatch = errors.New("currency mismatch between wallets or request")
)
