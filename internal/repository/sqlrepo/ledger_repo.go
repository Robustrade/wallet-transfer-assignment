package sqlrepo

import (
	"context"
	"fmt"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

type ledgerRepo struct {
	q Querier
	d Dialect
}

// Insert appends ledger entries. They are never updated or deleted.
func (r *ledgerRepo) Insert(ctx context.Context, entries ...domain.LedgerEntry) error {
	query := r.d.Rebind(
		`INSERT INTO ledger_entries (id, transfer_id, wallet_id, entry_type, amount, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	for _, e := range entries {
		if _, err := r.q.ExecContext(ctx, query,
			e.ID, e.TransferID, e.WalletID, string(e.Type), e.Amount, FormatTime(e.CreatedAt)); err != nil {
			return fmt.Errorf("insert %s ledger entry for transfer %s: %w", e.Type, e.TransferID, err)
		}
	}
	return nil
}
