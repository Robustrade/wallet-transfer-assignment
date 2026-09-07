package postgres

import (
	"context"
	"fmt"

	"Wallet_Transfer_Service/internal/domain"
)

type ledgerRepo struct {
	db dbtx
}

func (r *ledgerRepo) InsertPair(ctx context.Context, debit, credit *domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10)
		RETURNING id`
	rows, err := r.db.QueryContext(ctx, q,
		debit.TransferID, debit.WalletID, debit.EntryType, debit.Amount, debit.CreatedAt,
		credit.TransferID, credit.WalletID, credit.EntryType, credit.Amount, credit.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, 2)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("insert ledger entries: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}
	if len(ids) == 2 {
		debit.ID, credit.ID = ids[0], ids[1]
	}
	return nil
}

func (r *ledgerRepo) ListByTransfer(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	const q = `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries WHERE transfer_id = $1 ORDER BY id`
	rows, err := r.db.QueryContext(ctx, q, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var out []*domain.LedgerEntry
	for rows.Next() {
		e := &domain.LedgerEntry{}
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("list ledger entries: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
