package service

import "errors"

var (
	ErrInvalidIdempotencyKey = errors.New("idempotency key must contain 1 to 255 characters")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with a different request")
	ErrIncompleteIdempotencyRecord = errors.New("idempotency record has no stored response")
)
