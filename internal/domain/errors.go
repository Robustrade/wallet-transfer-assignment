package domain

import (
	"fmt"
	"net/http"
)

type ErrorCode string

const (
	ErrCodeInvalidInput           ErrorCode = "INVALID_INPUT"
	ErrCodeWalletNotFound         ErrorCode = "WALLET_NOT_FOUND"
	ErrCodeSameWalletTransfer     ErrorCode = "SAME_WALLET_TRANSFER"
	ErrCodeInvalidAmount          ErrorCode = "INVALID_AMOUNT"
	ErrCodeInsufficientFunds      ErrorCode = "INSUFFICIENT_FUNDS"
	ErrCodeIdempotencyKeyReused   ErrorCode = "IDEMPOTENCY_KEY_REUSED"
	ErrCodeTransferNotFound       ErrorCode = "TRANSFER_NOT_FOUND"
	ErrCodeInvalidStateTransition ErrorCode = "INVALID_STATE_TRANSITION"
	ErrCodeInternalError          ErrorCode = "INTERNAL_ERROR"
)

type AppError struct {
	Code       ErrorCode `json:"code"`
	Message    string    `json:"message"`
	HTTPStatus int       `json:"-"`
	Err        error     `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewAppError(code ErrorCode, message string, httpStatus int, err error) *AppError {
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        err,
	}
}

func ErrInvalidInput(msg string) *AppError {
	return NewAppError(ErrCodeInvalidInput, msg, http.StatusBadRequest, nil)
}

func ErrWalletNotFound(walletID string) *AppError {
	return NewAppError(ErrCodeWalletNotFound, fmt.Sprintf("wallet not found: %s", walletID), http.StatusNotFound, nil)
}

func ErrSameWalletTransfer() *AppError {
	return NewAppError(ErrCodeSameWalletTransfer, "source and destination wallets cannot be the same", http.StatusBadRequest, nil)
}

func ErrInvalidAmount(msg string) *AppError {
	return NewAppError(ErrCodeInvalidAmount, msg, http.StatusBadRequest, nil)
}

func ErrInsufficientFunds() *AppError {
	return NewAppError(ErrCodeInsufficientFunds, "source wallet has insufficient available balance", http.StatusUnprocessableEntity, nil)
}

func ErrIdempotencyKeyReused() *AppError {
	return NewAppError(ErrCodeIdempotencyKeyReused, "idempotency key reused with a different request payload", http.StatusConflict, nil)
}

func ErrTransferNotFound(transferID string) *AppError {
	return NewAppError(ErrCodeTransferNotFound, fmt.Sprintf("transfer not found: %s", transferID), http.StatusNotFound, nil)
}

func ErrInvalidStateTransition(from, to string) *AppError {
	return NewAppError(ErrCodeInvalidStateTransition, fmt.Sprintf("cannot transition transfer state from %s to %s", from, to), http.StatusUnprocessableEntity, nil)
}

func ErrInternal(err error) *AppError {
	return NewAppError(ErrCodeInternalError, "an unexpected internal server error occurred", http.StatusInternalServerError, err)
}
