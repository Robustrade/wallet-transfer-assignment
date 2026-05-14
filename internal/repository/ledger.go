package repository

import (
	"context"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/domain"
)

// LedgerRepository handles all persistence operations for ledger entries.
type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

// CreateEntries inserts the provided ledger entries.
// Callers must pass a transaction as dbtx so that ledger entries are written
// atomically with the corresponding balance updates and transfer status change.
func (r *LedgerRepository) CreateEntries(ctx context.Context, dbtx db.DBTX, entries []domain.LedgerEntry) error {
	const q = `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount) VALUES ($1, $2, $3, $4)`
	for _, e := range entries {
		if _, err := dbtx.Exec(ctx, q, e.TransferID, e.WalletID, e.Type, e.Amount); err != nil {
			return err
		}
	}
	return nil
}
