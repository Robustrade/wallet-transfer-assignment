package repository

import (
	"context"
	"wallet-transfer/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LedgerRepository interface {
	Create(
		ctx context.Context,
		tx pgx.Tx,
		entry *domain.LedgerEntry,
	) error
}

type ledgerRepository struct {
	db *pgxpool.Pool
}

func NewLedgerRepository(db *pgxpool.Pool) LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) Create(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {

	query := `
	INSERT INTO ledger_entries (
		id,
		transfer_id,
		wallet_id,
		type,
		amount
	) VALUES ($1, $2, $3, $4, $5)
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

	return err
}
