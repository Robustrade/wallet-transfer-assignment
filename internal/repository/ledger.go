package repository

import (
	"context"

	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

func (r *LedgerRepository) InsertLedgerEntries(ctx context.Context, db Executor, entries ...domain.LedgerEntry) error {
	insertQuery := `INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
	for _, entry := range entries {
		if _, err := db.Exec(ctx, insertQuery, entry.ID, entry.TransferID, entry.WalletID, entry.Type, entry.Amount, entry.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}
