package repository

import (
	"context"
	"database/sql"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

type LedgerRepository struct {
	db *sql.DB
}

func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

func (r *LedgerRepository) Create(
	ctx context.Context,
	tx *sql.Tx,
	entry *model.LedgerEntry,
) error {

	_, err := tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (
			id,
			transfer_id,
			wallet_id,
			entry_type,
			amount
		)
		VALUES ($1, $2, $3, $4, $5)
	`,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.EntryType,
		entry.Amount,
	)

	return err
}
