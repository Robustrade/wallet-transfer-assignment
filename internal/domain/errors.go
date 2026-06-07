// Package domain contains the core business entities, their state-transition
// rules and validation logic for the wallet transfer service.
//
// The domain layer is deliberately free of any I/O, framework or database
// concern so that the business rules can be reasoned about and unit tested in
// isolation.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the domain layer. Callers should compare against
// these using errors.Is so that the transport layer can map them to the
// correct HTTP status codes.
var (
	// ErrWalletNotFound is returned when a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrWalletAlreadyExists is returned when creating a wallet whose id is
	// already taken.
	ErrWalletAlreadyExists = errors.New("wallet already exists")

	// ErrTransferNotFound is returned when a referenced transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInsufficientFunds indicates the source wallet cannot cover a debit.
	// It is treated as a business outcome (a FAILED transfer), not a transport
	// level failure.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrIdempotencyKeyReused indicates that an idempotency key was replayed
	// with a different request payload, which is a client error.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different parameters")

	// ErrInvalidStateTransition indicates an attempt to move a transfer into a
	// state that is not reachable from its current state.
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)

// ValidationError represents a request that violates a business invariant such
// as a non-positive amount or a transfer to the same wallet. It carries the
// offending field so the API can return an actionable message.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// newValidationError is a small helper for constructing ValidationError values.
func newValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

// IsValidation reports whether err is (or wraps) a ValidationError.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}
