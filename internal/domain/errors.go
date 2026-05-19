package domain

import "errors"

// Sentinel errors returned by the domain and service layers. The handler maps
// these to transport-level responses; the service wraps them with %w so
// callers can match with errors.Is.
var (
	// ErrWalletNotFound: a referenced wallet does not exist (maps to HTTP 404).
	ErrWalletNotFound = errors.New("wallet not found")

	// ErrSameWallet: source and destination are the same wallet (HTTP 400).
	ErrSameWallet = errors.New("source and destination wallets must differ")

	// ErrInvalidAmount: amount is not strictly positive (HTTP 400).
	ErrInvalidAmount = errors.New("amount must be positive")

	// ErrMissingField: a required request field is empty (HTTP 400).
	ErrMissingField = errors.New("required field is missing")

	// ErrInsufficientFunds: source wallet cannot cover the amount. This is a
	// business outcome, not a request error: the transfer is persisted as
	// FAILED with this reason, not rejected at the transport layer.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrTransferNotFound: no transfer matches the given id/idempotency key.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrDuplicateIdempotencyKey: a transfer with the same idempotency key
	// already exists. The repository maps the Postgres unique-violation on
	// transfers.idempotency_key to this; the service treats it as "duplicate
	// request, return the existing transfer".
	ErrDuplicateIdempotencyKey = errors.New("duplicate idempotency key")
)
