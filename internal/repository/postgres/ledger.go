package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

const ledgerColumns = `entry_id, wallet_id, transfer_id, type, amount, created_at`

// LedgerRepository implements repository.LedgerRepository.
type LedgerRepository struct {
	db *sql.DB
}

// Create inserts a ledger entry (DEBIT or CREDIT).
func (r *LedgerRepository) Create(ctx context.Context, entry *domain.LedgerEntry) error {
	exec := dbOrTx(ctx, r.db)
	const query = `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1, $2, $3, $4)
		RETURNING entry_id, created_at
	`

	row := exec.QueryRowContext(
		ctx,
		query,
		entry.WalletID,
		entry.TransferID,
		string(entry.Type),
		entry.Amount,
	)
	if err := row.Scan(&entry.ID, &entry.CreatedAt); err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

// ListByTransferID returns all ledger rows for a transfer.
func (r *LedgerRepository) ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	exec := dbOrTx(ctx, r.db)
	query := `
		SELECT ` + ledgerColumns + `
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY entry_id
	`

	rows, err := exec.QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var entryType string
		if err := rows.Scan(&e.ID, &e.WalletID, &e.TransferID, &entryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		e.Type = domain.LedgerEntryType(entryType)
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}
	return entries, nil
}
