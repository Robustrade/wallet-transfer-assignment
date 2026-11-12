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
)
