package domain

import "errors"

// Sentinel errors returned by the domain and service layers. The handler
// layer maps these to HTTP status codes via internal/handler/errors.go;
// no other layer should match on error strings.
//
// New sentinels added here MUST also get a case in handler.classify or
// they will surface as 500s.
var (
	// ErrWalletNotFound is returned when an operation references a
	// wallet that does not exist. Handler maps this to 404.
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrInsufficientFunds is currently informational: the service
	// commits a FAILED transfer with this string as the reason instead
	// of returning the error to the caller, so users see a normal HTTP
	// 200 with the FAILED transfer body. Kept here in case a future
	// caller wants to short-circuit on it.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrSameWallet rejects a transfer whose source and destination
	// are the same. Schema CHECK also enforces this; the service
	// validation catches it earlier with a clearer message.
	ErrSameWallet = errors.New("from and to wallet must differ")

	// ErrInvalidAmount rejects zero or negative transfer amounts.
	// Schema CHECK also enforces `amount > 0`.
	ErrInvalidAmount = errors.New("amount must be positive")

	// ErrInvalidIdempotencyKey rejects an empty or missing
	// idempotencyKey. The key is mandatory for every money-moving
	// request — see AGENTS.md §S-9.
	ErrInvalidIdempotencyKey = errors.New("idempotency key is required")

	// ErrIdempotencyConflict is returned when a replay uses the same
	// key but a different request body. Handler maps this to 409.
	ErrIdempotencyConflict = errors.New("idempotency key reused with different request body")

	// ErrInvalidStateTransition is reserved for code paths that want
	// to validate state machine moves before issuing an UPDATE. The
	// current service always re-checks the state under a row lock so
	// this is unused in practice; left for future async/queued
	// processing variants.
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
