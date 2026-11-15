package domain

import (
	"errors"
	"fmt"
)

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func newValidationError(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

var (
	ErrInvalidStatusTransition = errors.New("invalid transfer status transition")
	ErrTransferNotProcessed    = errors.New("transfer is not processed")
	ErrInsufficientFunds       = errors.New("insufficient funds")
	ErrWalletNotFound          = errors.New("wallet not found")
	ErrWalletAlreadyExists     = errors.New("wallet already exists")
	ErrIdempotencyKeyReused    = errors.New("idempotency key was already used with a different request")
	ErrServiceBusy             = errors.New("the wallet is busy, please retry")
)
