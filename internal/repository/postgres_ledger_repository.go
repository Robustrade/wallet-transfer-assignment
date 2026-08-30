package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type PostgresLedgerRepository struct{}

func NewPostgresLedgerRepository() *PostgresLedgerRepository {
	return &PostgresLedgerRepository{}
}

func (r *PostgresLedgerRepository) Create(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {
	const query = `
		INSERT INTO ledger_entries (
			id,
			transfer_id,
			wallet_id,
			type,
			amount
		)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, err := tx.Exec(
		ctx,
		query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.Type,
		entry.Amount,
	)

	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}

	return nil
}
