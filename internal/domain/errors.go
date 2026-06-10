package domain

// DomainError represents a business logic error with a machine-readable code.
type DomainError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *DomainError) Error() string {
	return e.Message
}

// NewInvalidInputError creates a new validation error with a custom message.
func NewInvalidInputError(msg string) *DomainError {
	return &DomainError{Code: "INVALID_INPUT", Message: msg}
}

// Sentinel domain errors for common failure cases.
var (
	ErrWalletNotFound           = &DomainError{Code: "WALLET_NOT_FOUND", Message: "wallet not found"}
	ErrInsufficientFunds        = &DomainError{Code: "INSUFFICIENT_FUNDS", Message: "insufficient funds"}
	ErrDuplicateWallet          = &DomainError{Code: "DUPLICATE_WALLET", Message: "wallet already exists"}
	ErrTransferNotFound         = &DomainError{Code: "TRANSFER_NOT_FOUND", Message: "transfer not found"}
	ErrIdempotencyKeyMismatch   = &DomainError{Code: "IDEMPOTENCY_KEY_MISMATCH", Message: "idempotency key was already used with different request parameters"}
	ErrIdempotencyKeyInProgress = &DomainError{Code: "IDEMPOTENCY_KEY_IN_PROGRESS", Message: "a request with this idempotency key is already being processed"}
	ErrIdempotencyKeyNotFound   = &DomainError{Code: "IDEMPOTENCY_KEY_NOT_FOUND", Message: "idempotency key not found"}
	ErrInvalidStateTransition   = &DomainError{Code: "INVALID_STATE_TRANSITION", Message: "invalid state transition"}
)
