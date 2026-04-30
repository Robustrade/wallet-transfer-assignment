package repoiface

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
)

// TransferRepoIface persists and reads transfer rows.
type TransferRepoIface interface {
	// Claim attempts to insert a new PENDING transfer.
	//
	// On success (claimed=true), `transfer` is the just-inserted row with
	// DB-populated timestamps. On idempotency-key conflict (claimed=false),
	// `transfer` is the previously-committed row.
	//
	// Implementations MUST guarantee that the conflict-path transfer reflects
	// committed state — concurrent claims on the same idempotency_key must
	// not return a row from a still-running transaction.
	Claim(ctx context.Context, t model.Transfer) (claimed bool, transfer *model.Transfer, err error)

	// UpdateOutcome moves a transfer from PENDING to a terminal state and writes
	// the canonical currency. Returns the new updated_at timestamp so callers
	// can refresh their in-memory copy of the transfer.
	UpdateOutcome(
		ctx context.Context,
		id uuid.UUID,
		status model.TransferStatus,
		currency string,
		failureReason *string,
	) (updatedAt time.Time, err error)
}
