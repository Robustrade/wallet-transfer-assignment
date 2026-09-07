package utility

import "errors"

var (
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request")
	ErrRetryLater          = errors.New("request still in progress")
	ErrInsufficientFunds   = errors.New("insufficient funds")
)
