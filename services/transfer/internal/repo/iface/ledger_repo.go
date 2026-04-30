package repoiface

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
)

// LedgerRepoIface persists ledger entries.
type LedgerRepoIface interface {
	// Append inserts the supplied entries as a single SQL statement.
	// The UNIQUE(transfer_id, entry_type) constraint prevents duplicates if
	// the calling transaction is retried.
	Append(ctx context.Context, entries []model.LedgerEntry) error
}
