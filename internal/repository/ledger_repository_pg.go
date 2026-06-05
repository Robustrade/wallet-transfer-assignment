package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type ledgerRepo struct{}

func NewLedgerRepository() LedgerRepository {
	return &ledgerRepo{}
}

func (r *ledgerRepo) CreateEntry(
	ctx context.Context,
	tx pgx.Tx,
	entry *domain.LedgerEntry,
) error {

	_, err := tx.Exec(
		ctx,
		`
		INSERT INTO ledger_entries(
			id,
			transfer_id,
			wallet_id,
			entry_type,
			amount
		)
		VALUES($1,$2,$3,$4,$5)
		`,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.Type,
		entry.Amount,
	)

	return err
}
