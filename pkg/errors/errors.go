package errors

import "fmt"

// CustomError represents a domain-specific error with HTTP status code
type CustomError struct {
	Code       string
	Message    string
	StatusCode int
	Details    map[string]interface{}
}

func (e *CustomError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Common error codes and functions
var (
	// 400 Bad Request
	ErrInvalidRequest = &CustomError{
		Code:       "INVALID_REQUEST",
		Message:    "Invalid request parameters",
		StatusCode: 400,
	}

	ErrInvalidAmount = &CustomError{
		Code:       "INVALID_AMOUNT",
		Message:    "Amount must be positive",
		StatusCode: 400,
	}

	ErrInvalidWallet = &CustomError{
		Code:       "INVALID_WALLET",
		Message:    "Invalid wallet ID",
		StatusCode: 400,
	}

	ErrSelfTransfer = &CustomError{
		Code:       "SELF_TRANSFER",
		Message:    "Cannot transfer to the same wallet",
		StatusCode: 400,
	}

	ErrMissingIdempotencyKey = &CustomError{
		Code:       "MISSING_IDEMPOTENCY_KEY",
		Message:    "Idempotency key is required",
		StatusCode: 400,
	}

	// 402 Payment Required
	ErrInsufficientFunds = &CustomError{
		Code:       "INSUFFICIENT_FUNDS",
		Message:    "Source wallet has insufficient balance",
		StatusCode: 402,
	}

	// 404 Not Found
	ErrWalletNotFound = &CustomError{
		Code:       "WALLET_NOT_FOUND",
		Message:    "Wallet not found",
		StatusCode: 404,
	}

	ErrTransferNotFound = &CustomError{
		Code:       "TRANSFER_NOT_FOUND",
		Message:    "Transfer not found",
		StatusCode: 404,
	}

	// 409 Conflict
	ErrDuplicateTransfer = &CustomError{
		Code:       "DUPLICATE_TRANSFER",
		Message:    "Transfer with this idempotency key already exists",
		StatusCode: 409,
	}

	// 500 Internal Server Error
	ErrInternalError = &CustomError{
		Code:       "INTERNAL_ERROR",
		Message:    "Internal server error",
		StatusCode: 500,
	}

	ErrDatabaseError = &CustomError{
		Code:       "DATABASE_ERROR",
		Message:    "Database operation failed",
		StatusCode: 500,
	}

	ErrTransactionFailed = &CustomError{
		Code:       "TRANSACTION_FAILED",
		Message:    "Transaction execution failed",
		StatusCode: 500,
	}
)

// New creates a new CustomError with optional details
func New(code string, message string, statusCode int) *CustomError {
	return &CustomError{
		Code:       code,
		Message:    message,
		StatusCode: statusCode,
		Details:    make(map[string]interface{}),
	}
}

// WithDetails adds details to an error
func (e *CustomError) WithDetails(key string, value interface{}) *CustomError {
	newErr := &CustomError{
		Code:       e.Code,
		Message:    e.Message,
		StatusCode: e.StatusCode,
		Details:    make(map[string]interface{}),
	}
	// Copy existing details
	for k, v := range e.Details {
		newErr.Details[k] = v
	}
	// Add new detail
	newErr.Details[key] = value
	return newErr
}

// IsCustomError checks if error is a CustomError
func IsCustomError(err error) (*CustomError, bool) {
	ce, ok := err.(*CustomError)
	return ce, ok
}
