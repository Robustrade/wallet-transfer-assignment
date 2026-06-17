package domain

import "errors"

// Sentinel errors returned by the service and mapped to HTTP statuses by the
// handler, so transport- and driver-specific error types never cross layer
// boundaries.
//
// Note: insufficient funds is NOT an error here -- it is a recorded business
// outcome (the transfer is stored as FAILED and returned as a normal result),
// and structural validation failures are handled as 400s in the handler.
var (
	// ErrWalletNotFound means a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrIdempotencyConflict means the same idempotency key was reused with a
	// different request payload.
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different payload")
)
