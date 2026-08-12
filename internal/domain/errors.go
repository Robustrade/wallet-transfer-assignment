package domain

import "errors"

// Sentinel errors returned by the domain and service layers. The HTTP layer
// maps these to status codes with errors.Is / errors.As only — it never
// inspects a driver-level error.
var (
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrWalletAlreadyExists = errors.New("wallet already exists")
	ErrTransferNotFound    = errors.New("transfer not found")
	ErrCurrencyMismatch    = errors.New("currency mismatch")
	ErrIdempotencyKeyReuse = errors.New("idempotency key reused with a different request")
	ErrTransferInProgress  = errors.New("transfer is still in progress")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrIllegalStateChange  = errors.New("illegal transfer state transition")
	ErrResourceBusy        = errors.New("resource temporarily busy, retry")
)

// ValidationError reports a single field-level validation failure so the
// HTTP layer can echo back which field was wrong.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError builds a *ValidationError for the given field.
func NewValidationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}
