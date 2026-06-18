package apperrors

import "errors"

var ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
