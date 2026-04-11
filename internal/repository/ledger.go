package repository

import (
	"context"
	"fmt"

	"wallet-transfer/internal/domain"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

func (r *LedgerRepository) CreateEntries(ctx context.Context, exec Executor, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := exec.ExecContext(ctx,
			`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount, e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("create ledger entry: %w", err)
		}
	}
	return nil
}
