package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

func (r *DBRepository) CreateLedgerEntry(
	ctx context.Context,
	db *sql.Tx,
	entry *domain.LedgerEntry,
) error {
	const query = `
		INSERT INTO ledger_entries (
			id,
			transfer_id,
			wallet_id,
			entry_type,
			amount
		)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := db.ExecContext(
		ctx,
		query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.EntryType,
		entry.Amount,
	)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}

	return nil
}
