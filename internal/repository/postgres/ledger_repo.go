package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

type LedgerRepository struct {
	db *db
}

func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{db: &db{pool: pool}}
}

func (r *LedgerRepository) InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	batch := &pgx.Batch{}
	for _, e := range entries {
		batch.Queue(`
			INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
			VALUES ($1, $2, $3, $4, $5)
		`, e.ID, e.TransferID, e.WalletID, e.EntryType, e.Amount)
	}

	// Batches only run against a real connection, so route through the pool
	// or the active tx explicitly rather than the shared querier interface.
	var results pgx.BatchResults
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		results = tx.SendBatch(ctx, batch)
	} else {
		results = r.db.pool.SendBatch(ctx, batch)
	}
	defer results.Close()

	for range entries {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *LedgerRepository) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.q(ctx).Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at DESC
	`, walletID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
