package postgres

import (
	"context"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

type ledgerRepo struct {
	db dbtx
}

func (r *ledgerRepo) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			e.ID, e.WalletID, e.TransferID, e.Type, e.Amount, e.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (r *ledgerRepo) ListByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		FROM ledger_entries WHERE transfer_id = $1 ORDER BY created_at ASC`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLedgerEntries(rows)
}

func (r *ledgerRepo) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		FROM ledger_entries WHERE wallet_id = $1 ORDER BY created_at DESC`, walletID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLedgerEntries(rows)
}

func scanLedgerEntries(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]domain.LedgerEntry, error) {
	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &e.Type, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
