package domain

import "errors"

// Errors the service returns to callers. The handler maps each to an HTTP status.
var (
	ErrValidation           = errors.New("validation failed")
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")
	ErrInvalidTransition    = errors.New("invalid transfer status transition")
)

// ValidationError describes one invalid request field. It wraps ErrValidation,
// so callers can check errors.Is(err, ErrValidation).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

func (e *ValidationError) Unwrap() error { return ErrValidation }
