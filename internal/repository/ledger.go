package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
)

type LedgerStore interface {
	Create(ctx context.Context, entry domain.LedgerEntry) (domain.LedgerEntry, error)
	ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// LedgerRepository writes immutable ledger entries. Use the same transaction
// as wallet balance and transfer status changes.
type LedgerRepository struct{ db DBTX }

func NewLedgerRepository(db DBTX) *LedgerRepository { return &LedgerRepository{db: db} }

func (r *LedgerRepository) Create(ctx context.Context, entry domain.LedgerEntry) (domain.LedgerEntry, error) {
	if err := entry.Validate(); err != nil {
		return domain.LedgerEntry{}, fmt.Errorf("validate ledger entry: %w", err)
	}
	const query = `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, transfer_id, wallet_id, entry_type, amount`
	created, err := scanLedgerEntry(r.db.QueryRowContext(ctx, query,
		entry.ID, entry.TransferID, entry.WalletID, string(entry.Type), int64(entry.Amount)))
	if err != nil {
		return domain.LedgerEntry{}, fmt.Errorf("create ledger entry: %w", err)
	}
	return created, nil
}

func (r *LedgerRepository) ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	if transferID == "" {
		return nil, domain.ErrInvalidTransferID
	}
	const query = `
		SELECT id, transfer_id, wallet_id, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY CASE entry_type WHEN 'DEBIT' THEN 0 ELSE 1 END, id`
	rows, err := r.db.QueryContext(ctx, query, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries for transfer %q: %w", transferID, err)
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0, 2)
	for rows.Next() {
		entry, err := scanLedgerEntryFromRows(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}
	return entries, nil
}

func scanLedgerEntry(row *sql.Row) (domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	var entryType string
	var amount int64
	if err := row.Scan(&entry.ID, &entry.TransferID, &entry.WalletID, &entryType, &amount); err != nil {
		return domain.LedgerEntry{}, err
	}
	entry.Type = domain.EntryType(entryType)
	entry.Amount = domain.Amount(amount)
	if err := entry.Validate(); err != nil {
		return domain.LedgerEntry{}, fmt.Errorf("invalid ledger row: %w", err)
	}
	return entry, nil
}

func scanLedgerEntryFromRows(rows *sql.Rows) (domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	var entryType string
	var amount int64
	if err := rows.Scan(&entry.ID, &entry.TransferID, &entry.WalletID, &entryType, &amount); err != nil {
		return domain.LedgerEntry{}, fmt.Errorf("scan ledger entry: %w", err)
	}
	entry.Type = domain.EntryType(entryType)
	entry.Amount = domain.Amount(amount)
	if err := entry.Validate(); err != nil {
		return domain.LedgerEntry{}, fmt.Errorf("invalid ledger row: %w", err)
	}
	return entry, nil
}
