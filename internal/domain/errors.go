package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by the service. The HTTP layer maps them to status
// codes; storage implementations wrap them without losing identity.
var (
	// ErrWalletNotFound: at least one referenced wallet does not exist. This
	// outcome is recorded against the idempotency key (replays return 404).
	ErrWalletNotFound = errors.New("wallet not found")
	// ErrTransferNotFound: the requested transfer id does not exist (read
	// endpoints only; never recorded against an idempotency key).
	ErrTransferNotFound = errors.New("transfer not found")
	// ErrInsufficientFunds: the source wallet cannot cover the amount. The
	// FAILED transfer is persisted and the outcome is recorded against the
	// idempotency key (replays return 422).
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrKeyReused: the idempotency key already exists with a different
	// request payload. The request is rejected with 409 and never claims or
	// records anything.
	ErrKeyReused = errors.New("idempotency key already used with a different request")
	// ErrLockTimeout: a database lock (or speculative-insert wait) exceeded
	// the configured lock_timeout. Nothing was committed; the client may
	// retry the same request safely.
	ErrLockTimeout = errors.New("timed out waiting for a database lock")
	// ErrStatementTimeout: a statement exceeded statement_timeout. The
	// transaction is aborted and nothing was committed; the request is
	// retry-safe (503).
	ErrStatementTimeout = errors.New("statement timed out")
	// ErrDatabaseUnavailable: the database could not be reached or the
	// connection failed at the transport level (as opposed to a SQL-level
	// error, which stays a 500). Nothing was committed; retry-safe (503).
	ErrDatabaseUnavailable = errors.New("database temporarily unavailable")
	// ErrCommitOutcomeUnknown: the transaction's COMMIT produced an error, so
	// the outcome is unknown. Nothing may be reported as success unless a
	// follow-up read proves the outcome was recorded.
	ErrCommitOutcomeUnknown = errors.New("transaction commit outcome unknown")
	// ErrIncompleteIdempotency: a committed idempotency row has no recorded
	// outcome. Unreachable by design; surfaced as 500 if it ever happens.
	ErrIncompleteIdempotency = errors.New("idempotency record is incomplete")
)

// ValidationError describes a client-visible request defect. The HTTP layer
// maps it to 400 and uses Message as the response body's message.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// NewValidationError builds a ValidationError with a formatted message.
func NewValidationError(format string, args ...any) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}
