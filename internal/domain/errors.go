package domain

import "errors"

var (
	// ErrWalletNotFound indicates that the requested wallet ID does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrInsufficientFunds indicates that the source wallet balance is less than the transfer amount.
	ErrInsufficientFunds = errors.New("insufficient funds in source wallet")

	// ErrInvalidAmount indicates that the transfer amount is not greater than zero.
	ErrInvalidAmount = errors.New("transfer amount must be greater than zero")

	// ErrSameWalletTransfer indicates that fromWalletId and toWalletId are identical.
	ErrSameWalletTransfer = errors.New("source and destination wallets must be different")

	// ErrIdempotencyConflict indicates that a request with the same idempotency key is currently being processed.
	ErrIdempotencyConflict = errors.New("request with this idempotency key is currently in progress")

	// ErrIdempotencyKeyReusedMismatch indicates that the idempotency key was previously used with different request parameters.
	ErrIdempotencyKeyReusedMismatch = errors.New("idempotency key reused with different request payload")

	// ErrTransferNotFound indicates that the requested transfer ID does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInvalidWalletID indicates that a wallet ID parameter is empty.
	ErrInvalidWalletID = errors.New("wallet ID cannot be empty")
)
