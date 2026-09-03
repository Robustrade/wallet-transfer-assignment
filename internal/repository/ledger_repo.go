package repository

import (
	"context"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
)

type LedgerRepo struct{}

func NewLedgerRepo() *LedgerRepo {
	return &LedgerRepo{}
}

// InsertEntries writes the double-entry pair for a transfer. The UNIQUE
// (transfer_id, entry_type) constraint guarantees exactly one DEBIT and
// one CREDIT row can ever exist per transfer.
func (r *LedgerRepo) InsertEntries(ctx context.Context, q Querier, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := q.ExecContext(ctx, `
			INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
			VALUES ($1, $2, $3, $4)
		`, e.TransferID, e.WalletID, e.EntryType, e.Amount)
		if err != nil {
			return err
		}
	}
	return nil
}
