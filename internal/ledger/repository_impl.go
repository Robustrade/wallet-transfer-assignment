package ledger

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

type ledgerRepository struct {
	db *sql.DB
}

func NewLedgerRepository(db *sql.DB) LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) InsertLedgerEntries(ctx context.Context, tx *sql.Tx, entries []model.LedgerEntry) error {
	if len(entries) != 2 {
		return fmt.Errorf("ledger must always have exactly 2 entries")
	}
	query := `
		INSERT INTO ledger_entries (
			id,
			transfer_id,
			wallet_id,
			amount,
			created_at
		)
		VALUES ($1, $2, $3, $4, NOW()), ($5, $6, $7, $8, NOW())
		ON CONFLICT (transfer_id, wallet_id)
		DO NOTHING
	`

	_, err := tx.ExecContext(ctx, query,
		entries[0].Id, entries[0].TransferId, entries[0].WalletId, entries[0].Amount,
		entries[1].Id, entries[1].TransferId, entries[1].WalletId, entries[1].Amount,
	)
	return err
}

func (r *ledgerRepository) UpdateTransferStatus(ctx context.Context, tx *sql.Tx, transferId string, status string) error {
	query := `
		UPDATE transfers
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = 'PENDING'
	`
	res, err := tx.ExecContext(ctx, query, status, transferId)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		// already processed
		return nil
	}
	return err
}
