package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer-assignment/internal/domain"
)

func (t *TransferTx) CreateLedgerEntry(ctx context.Context, entry *domain.LedgerEntry) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1, $2, $3, $4)
	`, entry.WalletID, entry.TransferID, entry.Type, entry.Amount)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			if pgErr.Code == "23503" {
				return domain.ErrTransferNotFound
			}
		}

		return err
	}

	return nil
}
