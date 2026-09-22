// Package domain contains the core business entities and rules for the
// wallet transfer service. It has no dependency on transport or persistence
// concerns.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel domain errors. Callers should use errors.Is to check for these,
// since implementations may wrap them with additional context.
var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrWalletAlreadyExists    = errors.New("wallet already exists")
	ErrInsufficientFunds      = errors.New("insufficient funds")
	ErrSameWallet             = errors.New("source and destination wallet must differ")
	ErrInvalidAmount          = errors.New("amount must be greater than zero")
	ErrIdempotencyKeyRequired = errors.New("idempotencyKey is required")
	ErrIdempotencyKeyConflict = errors.New("idempotencyKey was already used with a different request payload")
	ErrIdempotencyInProgress  = errors.New("a request with this idempotencyKey is already being processed")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
	ErrInvalidWalletID        = errors.New("walletId is invalid")
)

// WalletNotFoundError identifies the wallet that could not be found while
// preserving errors.Is(err, ErrWalletNotFound) compatibility.
type WalletNotFoundError struct {
	ID string
}

func (e WalletNotFoundError) Error() string {
	return fmt.Sprintf("wallet %q not found", e.ID)
}

func (e WalletNotFoundError) Unwrap() error {
	return ErrWalletNotFound
}
