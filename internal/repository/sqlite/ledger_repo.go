package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer-assignment/internal/domain"
)

type ledgerRepo struct {
	tx *sql.Tx
}

func (r *ledgerRepo) InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := r.tx.ExecContext(ctx,
			`INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			e.ID, e.TransferID, e.WalletID, e.Type, e.Amount, e.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *ledgerRepo) ListByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.tx.QueryContext(ctx,
		`SELECT id, transfer_id, wallet_id, type, amount, created_at FROM ledger_entries WHERE transfer_id = ? ORDER BY created_at`,
		transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
