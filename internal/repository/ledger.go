package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

type LedgerRepository struct{}

// CreateEntries bulk-inserts a slice of ledger entries.
func (r *LedgerRepository) CreateEntries(ctx context.Context, tx pgx.Tx, entries []*domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4)`

	for _, e := range entries {
		_, err := tx.Exec(ctx, q,
			pgtype.UUID{Bytes: e.TransferID, Valid: true},
			pgtype.UUID{Bytes: e.WalletID, Valid: true},
			string(e.EntryType),
			amountToNumeric(e.Amount),
		)
		if err != nil {
			return fmt.Errorf("LedgerRepository.CreateEntries: %w", err)
		}
	}
	return nil
}
