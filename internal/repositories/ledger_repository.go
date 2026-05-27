package repositories

import (
	"database/sql"
	"fmt"
	"wallet-transfer-service/internal/models"
)

type ledgerRepository struct {
	db *sql.DB
}

// NewLedgerRepository creates a new ledger repository
func NewLedgerRepository(db *sql.DB) LedgerRepository {
	return &ledgerRepository{db: db}
}

// CreateEntry creates a new ledger entry
func (r *ledgerRepository) CreateEntry(tx *sql.Tx, entry *models.LedgerEntry) error {
	query := `INSERT INTO ledger_entries (id, wallet_id, transfer_id, entry_type, amount, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := tx.Exec(query,
		entry.ID,
		entry.WalletID,
		entry.TransferID,
		entry.EntryType,
		entry.Amount,
		entry.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create ledger entry: %w", err)
	}

	return nil
}

// GetEntriesByTransferID retrieves all ledger entries for a transfer
func (r *ledgerRepository) GetEntriesByTransferID(tx *sql.Tx, transferID string) ([]*models.LedgerEntry, error) {
	query := `SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
	          FROM ledger_entries WHERE transfer_id = $1 ORDER BY created_at`

	rows, err := tx.Query(query, transferID)
	if err != nil {
		return nil, fmt.Errorf("failed to get ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []*models.LedgerEntry
	for rows.Next() {
		var entry models.LedgerEntry
		err := rows.Scan(
			&entry.ID,
			&entry.WalletID,
			&entry.TransferID,
			&entry.EntryType,
			&entry.Amount,
			&entry.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		entries = append(entries, &entry)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating ledger entries: %w", err)
	}

	return entries, nil
}
