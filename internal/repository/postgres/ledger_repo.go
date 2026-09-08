package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"wallettransfer/internal/domain"
)

// LedgerRepo is the PostgreSQL implementation of service.LedgerRepo.
type LedgerRepo struct {
	db *sql.DB
}

func NewLedgerRepo(db *sql.DB) *LedgerRepo { return &LedgerRepo{db: db} }

// InsertEntries writes both legs of a double-entry transaction. The
// UNIQUE (transfer_id, wallet_id, type) constraint on ledger_entries means
// a retried insert for the same transfer would fail loudly rather than
// silently duplicating a leg — but in normal operation this is only ever
// called once per transfer, inside the same DB transaction as the balance
// update, so either both legs and the balance change land together or none
// of them do.
func (r *LedgerRepo) InsertEntries(ctx context.Context, tx *sql.Tx, entries []domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5, now())`
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, q, e.ID, e.TransferID, e.WalletID, e.Type, e.Amount); err != nil {
			return fmt.Errorf("insert ledger entry (%s/%s): %w", e.WalletID, e.Type, err)
		}
	}
	return nil
}
