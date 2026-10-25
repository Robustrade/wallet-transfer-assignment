package domain

import "errors"

var (
	// ErrInvalidRequest wraps every input validation failure.
	ErrInvalidRequest = errors.New("invalid request")

	ErrWalletNotFound   = errors.New("wallet not found")
	ErrTransferNotFound = errors.New("transfer not found")
	ErrWalletExists     = errors.New("wallet already exists")

	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrBalanceOverflow   = errors.New("balance overflow")

	// ErrInvalidTransition is returned when a transfer state change is not
	// allowed, e.g. processing a transfer that is already PROCESSED.
	ErrInvalidTransition = errors.New("invalid transfer state transition")

	// ErrIdempotencyKeyReused is returned when a key is replayed with a
	// different payload than the request that first used it.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")
)
