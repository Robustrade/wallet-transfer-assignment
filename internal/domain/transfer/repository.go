package transfer

import "context"

// Repository defines the persistence contract for the transfer domain.
// Concrete implementations live in internal/platform/postgres.
// No DB driver is imported here.
type Repository interface {
	// Create persists a new transfer record.
	Create(ctx context.Context, t *Transfer) error

	// GetByID returns a transfer by its internal ID.
	GetByID(ctx context.Context, id string) (*Transfer, error)

	// GetByTxnID looks up a transfer by its caller-supplied idempotency key.
	// Returns nil, nil when no match exists (not an error — just a cache miss).
	GetByTxnID(ctx context.Context, txnID string) (*Transfer, error)

	// UpdateStatus persists a status change (and optional failure reason) for a transfer.
	// The update is a no-op if the transfer is already in a terminal state.
	UpdateStatus(ctx context.Context, id string, status Status, failureReason string) error
}
