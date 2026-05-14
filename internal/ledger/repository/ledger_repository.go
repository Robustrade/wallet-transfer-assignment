package repository

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer/internal/ledger/domain"
)

// LedgerRepository handles ledger entry database operations
type LedgerRepository struct {
	db *sql.DB
}

// NewLedgerRepository creates a new ledger repository
func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// CreateEntry inserts a new ledger entry
func (r *LedgerRepository) CreateEntry(ctx context.Context, tx *sql.Tx, entry *domain.LedgerEntry) error {
	query := `
		INSERT INTO ledger_entries (entry_id, transfer_id, wallet_id, entry_type, amount_cents, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.ExecContext(ctx, query,
		entry.ID,
		entry.TransferID,
		entry.WalletID,
		entry.Type,
		entry.Amount,
		entry.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create ledger entry: %w", err)
	}
	return nil
}

// GetEntriesByTransfer retrieves all ledger entries for a transfer
func (r *LedgerRepository) GetEntriesByTransfer(ctx context.Context, tx *sql.Tx, transferID string) ([]domain.LedgerEntry, error) {
	query := `
		SELECT entry_id, transfer_id, wallet_id, entry_type, amount_cents, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at ASC
	`
	rows, err := tx.QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		err := rows.Scan(
			&entry.ID,
			&entry.TransferID,
			&entry.WalletID,
			&entry.Type,
			&entry.Amount,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

// GetWalletLedger retrieves ledger entries for a wallet
func (r *LedgerRepository) GetWalletLedger(ctx context.Context, tx *sql.Tx, walletID string, limit int, offset int) ([]domain.LedgerEntry, error) {
	query := `
		SELECT entry_id, transfer_id, wallet_id, entry_type, amount_cents, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := tx.QueryContext(ctx, query, walletID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet ledger: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		err := rows.Scan(
			&entry.ID,
			&entry.TransferID,
			&entry.WalletID,
			&entry.Type,
			&entry.Amount,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, rows.Err()
}
