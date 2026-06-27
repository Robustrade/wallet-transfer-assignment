package postgres

import (
	"context"
	"fmt"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
	"github.com/ferozyk/wallet-transfer-assignment/internal/service"
)

// LedgerRepository is the PostgreSQL adapter for double-entry ledger rows.
type LedgerRepository struct {
	db *DB
}

// NewLedgerRepository constructs a LedgerRepository over db.
func NewLedgerRepository(db *DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

var _ service.LedgerRepository = (*LedgerRepository)(nil)

// CreateEntries inserts ledger entries atomically. When invoked inside an
// ambient transaction (the normal case) the batch participates in it, so the
// balanced pair is written together or not at all.
func (r *LedgerRepository) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// The entries are written through the ambient transaction (conn resolves to
	// the active tx), so the multiple inserts are atomic as a unit.
	conn := r.db.conn(ctx)
	for _, e := range entries {
		if _, err := conn.Exec(ctx,
			`INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount, created_at)
			 VALUES ($1, $2, $3, $4, $5, now())`,
			e.ID, e.WalletID, e.TransferID, string(e.Type), e.Amount,
		); err != nil {
			return fmt.Errorf("postgres: create ledger entries: %w", err)
		}
	}
	return nil
}

// ListByWallet returns the ledger history for a wallet, newest first.
func (r *LedgerRepository) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.conn(ctx).Query(ctx,
		`SELECT id, wallet_id, transfer_id, type, amount, created_at
		   FROM ledger_entries
		  WHERE wallet_id = $1
		  ORDER BY created_at DESC, id DESC`,
		walletID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var (
			e         domain.LedgerEntry
			entryType string
		)
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &entryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan ledger entry: %w", err)
		}
		e.Type = domain.EntryType(entryType)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate ledger entries: %w", err)
	}
	return entries, nil
}
