// Package domain holds entities, the transfer state machine and business rules.
// It has no database or HTTP code.
package domain

import "errors"

// Sentinel errors for expected business failures. Wrap them with fmt.Errorf("%w: ...")
// to add detail; callers check them with errors.Is.
var (
	ErrValidation             = errors.New("validation error")
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrWalletAlreadyExists    = errors.New("wallet already exists")
	ErrTransferNotFound       = errors.New("transfer not found")
	ErrIdempotencyKeyReused   = errors.New("idempotency key was already used with a different request")
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
