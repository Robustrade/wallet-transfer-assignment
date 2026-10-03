package domain

import "errors"

// Errors for requests that break a rule. The handler maps each one to its
// API error code.
var (
	ErrInvalidField  = errors.New("invalid field")
	ErrInvalidAmount = errors.New("amount must be an integer from 1 to 9223372036854775807")
	ErrSameWallet    = errors.New("fromWalletId and toWalletId must differ")

	ErrWalletNotFound       = errors.New("fromWalletId or toWalletId does not exist")
	ErrIdempotencyKeyReused = errors.New("idempotencyKey was already used with a different body")
)
