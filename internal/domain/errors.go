// Package domain holds the wallet transfer entities, their state machine and
// business rules. It has no knowledge of HTTP or persistence.
package domain

import "errors"

// Sentinel errors shared across layers. Callers match them with errors.Is.
var (
	// ErrWalletNotFound means a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")
	// ErrTransferNotFound means the requested transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")
	// ErrIdempotencyKeyReused means the key was already used for a different request.
	ErrIdempotencyKeyReused = errors.New("idempotency key was already used with a different request")
	// ErrInvalidTransition means a state change is not allowed from the current state.
	ErrInvalidTransition = errors.New("invalid transfer state transition")
	// ErrBusy means the operation could not obtain a lock in time and is safe to retry.
	ErrBusy = errors.New("resource busy, retry later")
)

// ValidationError reports a request that violates a business rule.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func invalid(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}
