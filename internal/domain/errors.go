package domain

import "errors"

var (
	// ErrInsufficientBalance is returned when a wallet does not have enough funds.
	ErrInsufficientBalance = errors.New("insufficient balance")

	// ErrWalletNotFound is returned when a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrInvalidAmount is returned when the transfer amount is not positive.
	ErrInvalidAmount = errors.New("amount must be positive")

	// ErrSameWallet is returned when source and destination wallets are the same.
	ErrSameWallet = errors.New("cannot transfer to the same wallet")

	// ErrInvalidRequest is returned when required request fields are missing.
	ErrInvalidRequest = errors.New("invalid request: missing required fields")
)
