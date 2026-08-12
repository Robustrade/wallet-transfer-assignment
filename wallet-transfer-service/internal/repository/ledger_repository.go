package repository

import (
	"context"
	"fmt"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/google/uuid"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository { return &LedgerRepository{} }

func (r *LedgerRepository) Create(ctx context.Context, db DBTX, entry domain.LedgerEntry) error {
	const q = `
        INSERT INTO ledger_entries
            (id, transfer_id, wallet_id, type, amount)
        VALUES ($1, $2, $3, $4, $5)`

	_, err := db.Exec(ctx, q, entry.ID, entry.TransferID, entry.WalletID, entry.Type, entry.Amount)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	return nil
}

func (r *LedgerRepository) CountByTransfer(ctx context.Context, db DBTX, transferID uuid.UUID) (int, error) {
	const q = `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`
	var count int
	if err := db.QueryRow(ctx, q, transferID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count ledger entries: %w", err)
	}
	return count, nil
}
