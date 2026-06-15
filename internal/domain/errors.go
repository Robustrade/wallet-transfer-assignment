package domain

import "fmt"

type ErrorCode string

const (
	ErrorInvalidRequest            ErrorCode = "invalid_request"
	ErrorIdempotencyConflict       ErrorCode = "idempotency_key_conflict"
	ErrorSourceWalletNotFound      ErrorCode = "source_wallet_not_found"
	ErrorDestinationWalletNotFound ErrorCode = "destination_wallet_not_found"
	ErrorInsufficientFunds         ErrorCode = "insufficient_funds"
	ErrorInvalidTransferState      ErrorCode = "invalid_transfer_state"
	ErrorTransferNotFound          ErrorCode = "transfer_not_found"
	ErrorWalletNotFound            ErrorCode = "wallet_not_found"
	ErrorWalletAlreadyExists       ErrorCode = "wallet_already_exists"
	ErrorPersistenceFailure        ErrorCode = "persistence_failure"
)

type AppError struct {
	Code    ErrorCode
	Message string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func NewAppError(code ErrorCode, message string) *AppError {
	return &AppError{Code: code, Message: message}
}
