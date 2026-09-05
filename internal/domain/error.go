package domain

import "errors"

var (
	ErrWalletNotFound = errors.New("wallet not found")

	ErrInsufficientFunds = errors.New("insufficient funds")

	ErrSameWallet = errors.New(
		"source and destination wallets must be different",
	)

	ErrInvalidAmount = errors.New(
		"transfer amount must be greater than zero",
	)

	ErrIdempotencyKeyRequired = errors.New(
		"idempotency key is required",
	)

	ErrIdempotencyConflict = errors.New(
		"idempotency key already used with different request",
	)

	ErrInvalidTransferState = errors.New(
		"invalid transfer state transition",
	)
)
