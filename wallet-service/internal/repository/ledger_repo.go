package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/shubh/wallet-service/internal/db"
	"github.com/shubh/wallet-service/internal/domain"
)

// LedgerRepository handles ledger entry persistence operations.
type LedgerRepository struct {
	db *sql.DB
}

// NewLedgerRepository creates a new LedgerRepository.
func NewLedgerRepository(db *sql.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// CreatePair inserts both the debit and credit ledger entries within a transaction.
func (r *LedgerRepository) CreatePair(ctx context.Context, transferID string, fromWalletID, toWalletID string, amount int64) error {
	dbtx := db.GetTxFromContext(ctx, r.db)

	// Debit entry (money leaves sender)
	_, err := dbtx.ExecContext(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		 VALUES ($1, $2, $3, $4)`,
		transferID, fromWalletID, domain.LedgerEntryTypeDebit, amount,
	)
	if err != nil {
		return fmt.Errorf("failed to create debit ledger entry: %w", err)
	}

	// Credit entry (money arrives at receiver)
	_, err = dbtx.ExecContext(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		 VALUES ($1, $2, $3, $4)`,
		transferID, toWalletID, domain.LedgerEntryTypeCredit, amount,
	)
	if err != nil {
		return fmt.Errorf("failed to create credit ledger entry: %w", err)
	}

	return nil
}

// GetByTransferID retrieves all ledger entries for a given transfer.
func (r *LedgerRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		 FROM ledger_entries
		 WHERE transfer_id = $1
		 ORDER BY entry_type`,
		transferID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
