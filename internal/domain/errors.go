package domain

import "errors"

// Sentinel errors returned by the service layer. The HTTP handler maps these
// to specific status codes; keeping them as package-level vars lets callers
// use errors.Is instead of matching on strings.
var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrInvalidAmount          = errors.New("amount must be a positive integer")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrIdempotencyKeyRequired = errors.New("idempotencyKey is required")
	ErrIdempotencyKeyConflict = errors.New("idempotencyKey was reused with a different request payload")
	ErrDuplicateInFlight      = errors.New("a request with this idempotencyKey is already being processed, try again shortly")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
