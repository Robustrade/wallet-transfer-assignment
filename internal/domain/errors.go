package domain

import (
	"errors"
	"fmt"
)

// Standard domain error codes
const (
	CodeInvalidRequest              = "INVALID_REQUEST"
	CodeWalletNotFound              = "WALLET_NOT_FOUND"
	CodeInsufficientFunds           = "INSUFFICIENT_FUNDS"
	CodeSameWalletTransfer          = "SAME_WALLET_TRANSFER"
	CodeInvalidAmount               = "INVALID_AMOUNT"
	CodeIdempotencyKeyReuseMismatch = "IDEMPOTENCY_KEY_REUSE_MISMATCH"
	CodeTransferNotFound            = "TRANSFER_NOT_FOUND"
	CodeInternalError               = "INTERNAL_ERROR"
)

// DomainError represents a structured, domain-specific error.
type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *DomainError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewDomainError creates a new DomainError instance.
func NewDomainError(code, message string) *DomainError {
	return &DomainError{
		Code:    code,
		Message: message,
	}
}

// Predefined domain errors
var (
	ErrMissingIdempotencyKey = &DomainError{
		Code:    CodeInvalidRequest,
		Message: "idempotencyKey is required and cannot be empty",
	}

	ErrMissingFromWalletID = &DomainError{
		Code:    CodeInvalidRequest,
		Message: "fromWalletId is required and cannot be empty",
	}

	ErrMissingToWalletID = &DomainError{
		Code:    CodeInvalidRequest,
		Message: "toWalletId is required and cannot be empty",
	}

	ErrSameWalletTransfer = &DomainError{
		Code:    CodeSameWalletTransfer,
		Message: "fromWalletId and toWalletId must be different",
	}

	ErrInvalidAmount = &DomainError{
		Code:    CodeInvalidAmount,
		Message: "amount must be a positive integer in paise (greater than zero)",
	}

	ErrWalletNotFound = &DomainError{
		Code:    CodeWalletNotFound,
		Message: "one or more specified wallets were not found",
	}

	ErrInsufficientFunds = &DomainError{
		Code:    CodeInsufficientFunds,
		Message: "insufficient funds in source wallet",
	}

	ErrIdempotencyKeyReuseMismatch = &DomainError{
		Code:    CodeIdempotencyKeyReuseMismatch,
		Message: "idempotency key was previously used with different transfer parameters",
	}

	ErrTransferNotFound = &DomainError{
		Code:    CodeTransferNotFound,
		Message: "transfer not found",
	}
)

// IsDomainError checks if an error is a DomainError.
func IsDomainError(err error) bool {
	var de *DomainError
	return errors.As(err, &de)
}
