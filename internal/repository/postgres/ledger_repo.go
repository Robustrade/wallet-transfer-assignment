package postgres

import (
	"context"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
)

type LedgerRepo struct {
	db DBTX
}

func NewLedgerRepo(db DBTX) *LedgerRepo {
	return &LedgerRepo{db: db}
}

func (r *LedgerRepo) InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount)
			VALUES ($1, $2, $3, $4, $5)
		`, e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount); err != nil {
			return err
		}
	}
	return nil
}

func (r *LedgerRepo) ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at
	`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		var entryType string
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &entryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Type = domain.LedgerEntryType(entryType)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
