package domain

import (
	"fmt"
	"strings"
)

const (
	CodeValidation        = "VALIDATION_ERROR"
	CodeWalletNotFound    = "WALLET_NOT_FOUND"
	CodeTransferNotFound  = "TRANSFER_NOT_FOUND"
	CodeWalletExists      = "WALLET_EXISTS"
	CodeIdempotencyReused = "IDEMPOTENCY_KEY_REUSED"
	CodeInsufficientFunds = "INSUFFICIENT_FUNDS"
	CodeAmountOverflow    = "AMOUNT_OVERFLOW"
	CodeBusy              = "RESOURCE_BUSY"
	CodeInternal          = "INTERNAL_ERROR"
)

// Sentinel errors for errors.Is checks. Typed errors below also match these.
var (
	ErrNotFound            = fmt.Errorf("not found")
	ErrWalletNotFound      = fmt.Errorf("wallet not found")
	ErrTransferNotFound    = fmt.Errorf("transfer not found")
	ErrConflict            = fmt.Errorf("conflict")
	ErrWalletExists        = fmt.Errorf("wallet already exists")
	ErrIdempotencyMismatch = fmt.Errorf("idempotency key reused with a different request")
	ErrInsufficientFunds   = fmt.Errorf("insufficient funds")
	ErrInvalidTransition   = fmt.Errorf("invalid transfer state transition")
	ErrAmountOverflow      = fmt.Errorf("balance overflow")
	ErrBusy                = fmt.Errorf("resource busy")
	ErrInvariant           = fmt.Errorf("data invariant violated")
)

// FieldError is one validation failure tied to a request or entity field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError is safe to show to API clients.
type ValidationError struct {
	Details []FieldError
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Details) == 0 {
		return "validation failed"
	}
	parts := make([]string, len(e.Details))
	for i, detail := range e.Details {
		parts[i] = detail.Field + ": " + detail.Message
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

// NotFoundError identifies a missing wallet or transfer.
type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	if e == nil {
		return "not found"
	}
	return fmt.Sprintf("%s %q not found", e.Entity, e.ID)
}

func (e *NotFoundError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	switch target {
	case ErrNotFound:
		return true
	case ErrWalletNotFound:
		return e.Entity == "wallet"
	case ErrTransferNotFound:
		return e.Entity == "transfer"
	default:
		return false
	}
}

// ConflictError identifies a unique-key collision that is safe to show.
type ConflictError struct {
	Entity string
	ID     string
}

func (e *ConflictError) Error() string {
	if e == nil {
		return "conflict"
	}
	return fmt.Sprintf("%s %q already exists", e.Entity, e.ID)
}

func (e *ConflictError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	switch target {
	case ErrConflict:
		return true
	case ErrWalletExists:
		return e.Entity == "wallet"
	default:
		return false
	}
}
