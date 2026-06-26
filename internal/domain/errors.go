package domain

import "errors"

// Sentinel errors expressed in domain terms. Outer layers (service, handler)
// inspect these with errors.Is to make transport and persistence decisions
// without depending on lower-level error types.
var (
	// ErrWalletNotFound indicates a referenced wallet does not exist.
	ErrWalletNotFound = errors.New("wallet not found")
	// ErrTransferNotFound indicates a referenced transfer does not exist.
	ErrTransferNotFound = errors.New("transfer not found")

	// ErrInsufficientFunds indicates the source wallet cannot cover the debit.
	ErrInsufficientFunds = errors.New("insufficient funds")

	// ErrInvalidAmount indicates a non-positive transfer amount.
	ErrInvalidAmount = errors.New("amount must be a positive integer")
	// ErrSameWallet indicates the source and destination wallets are identical.
	ErrSameWallet = errors.New("source and destination wallets must differ")
	// ErrMissingIdempotencyKey indicates a request without an idempotency key.
	ErrMissingIdempotencyKey = errors.New("idempotency key is required")
	// ErrMissingWalletID indicates a request with an empty wallet identifier.
	ErrMissingWalletID = errors.New("wallet id is required")

	// ErrDuplicateIdempotencyKey is returned by the persistence layer when an
	// idempotency key already exists. The service treats this as the signal to
	// replay the original result rather than execute a new transfer.
	ErrDuplicateIdempotencyKey = errors.New("idempotency key already exists")
	// ErrIdempotencyKeyReuse indicates the same idempotency key was presented
	// with a different request payload, which is a client error.
	ErrIdempotencyKeyReuse = errors.New("idempotency key reused with a different request")

	// ErrInvalidStateTransition indicates an illegal transfer lifecycle change.
	ErrInvalidStateTransition = errors.New("invalid transfer state transition")
)
