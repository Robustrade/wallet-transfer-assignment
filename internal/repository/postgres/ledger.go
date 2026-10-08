package postgres

import (
	"context"
	"fmt"

	"wallet-transfer-assignment/internal/domain"
)

func (t *transaction) CreateLedgerEntry(ctx context.Context, v domain.LedgerEntry) error {
	if err := v.Validate(); err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	_, err := t.exec(ctx, "create ledger entry", `INSERT INTO ledger_entries (id,wallet_id,transfer_id,entry_type,amount,created_at) VALUES ($1,$2,$3,$4,$5,$6)`, v.ID, v.WalletID, v.TransferID, v.Type, v.Amount, v.CreatedAt)
	return err
}
func (t *transaction) ListLedgerEntries(ctx context.Context, id string) ([]domain.LedgerEntry, error) {
	if t.failure != nil {
		return nil, t.failure
	}
	rows, err := t.tx.QueryContext(ctx, `SELECT id,wallet_id,transfer_id,entry_type,amount,created_at FROM ledger_entries WHERE transfer_id=$1 ORDER BY entry_type,id`, id)
	if err != nil {
		return nil, t.record("list ledger entries", err)
	}
	defer rows.Close()
	entries := make([]domain.LedgerEntry, 0)
	for rows.Next() {
		var v domain.LedgerEntry
		if err := rows.Scan(&v.ID, &v.WalletID, &v.TransferID, &v.Type, &v.Amount, &v.CreatedAt); err != nil {
			return nil, t.record("scan ledger entry", err)
		}
		entries = append(entries, v)
	}
	if err := rows.Err(); err != nil {
		return nil, t.record("read ledger entries", err)
	}
	return entries, nil
}
