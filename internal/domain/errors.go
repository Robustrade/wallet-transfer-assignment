package domain

import "errors"

var (
	ErrInvalidRequest     = errors.New("invalid request")
	ErrSameWallet         = errors.New("from and to wallet must differ")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrWalletNotFound     = errors.New("wallet not found")
	ErrTransferNotFound   = errors.New("transfer not found")
	ErrInvalidAmount      = errors.New("amount must be positive")
	ErrMissingIdempotency = errors.New("idempotency key is required")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

func NewValidationError(msg string) ValidationError {
	return ValidationError{Message: msg}
}
