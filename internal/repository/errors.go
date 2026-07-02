package repository

import "errors"

var (
	ErrNotFound             = errors.New("not found")
	ErrDuplicateIdempotency = errors.New("duplicate idempotency key")
)
