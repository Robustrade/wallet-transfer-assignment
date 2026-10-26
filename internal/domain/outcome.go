package domain

import "errors"

const (
	StatusProcessed         = "PROCESSED"
	StatusFailed            = "FAILED"
	ReasonInsufficientFunds = "INSUFFICIENT_FUNDS"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used for a different transfer")
	ErrWalletNotFound      = errors.New("source or destination wallet does not exist")
	ErrInvariantViolation  = errors.New("unexpected database invariant violation")
)

// TransferResult is formed from durable, replayable transfer fields.
// No live wallet balance or time-dependent data is included.
type TransferResult struct {
	ID            string
	Status        string
	FailureReason string
}

// ErrRetryableTransaction means PostgreSQL confirmed that the transaction
// was aborted (deadlock or serialization failure). It is not used for
// ambiguous connection/COMMIT errors, which clients retry using the same key.
var ErrRetryableTransaction = errors.New("transaction aborted by PostgreSQL; retry entire transaction")
