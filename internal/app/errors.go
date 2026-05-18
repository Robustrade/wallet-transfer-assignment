package app

import (
	"errors"
	"strings"
)

var (
	ErrInvalidRequest       = errors.New("invalid request")
	ErrWalletNotFound       = errors.New("wallet not found")
	ErrInsufficientFunds    = errors.New("insufficient funds")
	ErrDuplicateIdempotency = errors.New("duplicate idempotency key")
)

func IsUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "unique constraint")
}
