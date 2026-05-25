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

func (r *TransferRepository) ListWalletLedgerEntries(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	var walletExists bool
	if err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM wallets
			WHERE wallet_id = $1
		)
	`, walletID).Scan(&walletExists); err != nil {
		return nil, err
	}
	if !walletExists {
		return nil, domain.ErrWalletNotFound
	}

	rows, err := r.db.Query(ctx, `
		SELECT entry_id, wallet_id, transfer_id, type, amount
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at, entry_id
	`, walletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0)
	for rows.Next() {
		var entry domain.LedgerEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.WalletID,
			&entry.TransferID,
			&entry.Type,
			&entry.Amount,
		); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
