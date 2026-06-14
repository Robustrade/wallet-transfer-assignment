package repository

import (
	"context"
	"fmt"

	"github.com/wallet-transfer-assignment/internal/domain"
)

// LedgerEntryRepository handles persistence for ledger entries.
type LedgerEntryRepository struct {
	db DBTX
}

func NewLedgerEntryRepository(db DBTX) *LedgerEntryRepository {
	return &LedgerEntryRepository{db: db}
}

// CreatePair inserts the debit and credit entries for a transfer in a single
// round-trip using a multi-row INSERT. The caller must supply entries produced
// by domain.NewLedgerPair to guarantee the double-entry invariant.
func (r *LedgerEntryRepository) CreatePair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	const q = `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
		VALUES
		    ($1, $2, $3, $4, $5),
		    ($6, $7, $8, $9, $10)`

	_, err := r.db.Exec(ctx, q,
		debit.ID, debit.TransferID, debit.WalletID, debit.Type, debit.Amount,
		credit.ID, credit.TransferID, credit.WalletID, credit.Type, credit.Amount,
	)
	if err != nil {
		return fmt.Errorf("ledger.CreatePair: %w", err)
	}
	return nil
}

// GetByTransferID returns both ledger entries for the given transfer.
func (r *LedgerEntryRepository) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	const q = `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM   ledger_entries
		WHERE  transfer_id = $1
		ORDER BY type`

	rows, err := r.db.Query(ctx, q, transferID)
	if err != nil {
		return nil, fmt.Errorf("ledger.GetByTransferID: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("ledger.GetByTransferID scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
