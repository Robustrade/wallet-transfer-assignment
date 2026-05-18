package repository

import (
	"context"

	"wallet-transfer-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type LedgerRepository struct {
}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

func (r *LedgerRepository) CreateLedgerEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {

	query := `
		INSERT INTO ledger_entries (
			id,
			wallet_id,
			transfer_id,
			entry_type,
			amount,
			created_at
		)
		VALUES (
			$1, $2, $3, $4, $5, NOW()
		)
	`

	_, err := tx.Exec(
		ctx,
		query,
		entry.ID,
		entry.WalletID,
		entry.TransferID,
		entry.EntryType,
		entry.Amount,
	)

	return err
}
