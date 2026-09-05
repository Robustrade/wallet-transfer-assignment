package domain

import "errors"

var (
	ErrValidation                = errors.New("validation failed")
	ErrWalletNotFound            = errors.New("wallet not found")
	ErrTransferNotFound          = errors.New("transfer not found")
	ErrInsufficientFunds         = errors.New("insufficient funds")
	ErrCurrencyMismatch          = errors.New("currency mismatch")
	ErrIdempotencyKeyConflict    = errors.New("idempotency key reused with a different payload")
	ErrIdempotencyRecordNotFound = errors.New("no idempotency record")
	ErrIdempotencyKeyAlreadyUsed = errors.New("idempotency key already claimed")
	ErrConflict                  = errors.New("conflicting concurrent operation, retry")
	ErrTimeout                   = errors.New("operation timed out")
	ErrInvalidStateTransition    = errors.New("invalid transfer state transition")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func (e *ValidationError) Unwrap() error { return ErrValidation }

func newValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
