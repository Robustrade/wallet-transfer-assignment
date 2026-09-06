package repository

import (
	"context"
	"database/sql"

	"wallet-transfer-assignment/internal/domain"
)

type ledgerRepository struct{}

func NewLedgerRepository() LedgerRepository {
	return &ledgerRepository{}
}

func (r *ledgerRepository) Create(ctx context.Context, tx *sql.Tx, entry *domain.LedgerEntry) error {
	const query = `
		INSERT INTO ledger_entries (
			id, transfer_id, wallet_id, entry_type, amount, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := tx.ExecContext(ctx, query, entry.Id, entry.TransferId, entry.WalletId,
		entry.EntryType, entry.Amount, entry.CreatedAt)

	return err
}
