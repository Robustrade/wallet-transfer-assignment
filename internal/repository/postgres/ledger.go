package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/domain"
)

type ledgerRepo struct{ db dbtx }

func (r *ledgerRepo) Insert(ctx context.Context, entries ...domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := r.db.Exec(ctx,
			`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
			 VALUES ($1, $2, $3, $4, $5)`,
			e.TransferID, e.WalletID, string(e.Type), e.Amount, e.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *ledgerRepo) ListByTransfer(ctx context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, transfer_id, wallet_id, type, amount, created_at
		   FROM ledger_entries WHERE transfer_id = $1 ORDER BY id`, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var (
			e domain.LedgerEntry
			t string
		)
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &t, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		e.Type = domain.EntryType(t)
		e.CreatedAt = e.CreatedAt.UTC()
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	return entries, nil
}
