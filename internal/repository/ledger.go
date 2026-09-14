package repository

import (
	"context"
	"fmt"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

// InsertLedgerEntries writes a transfer's ledger entries.
//
// They are written in the transfer's own transaction, so the ledger and the
// balances it explains commit together or not at all. UNIQUE (transfer_id,
// type) rejects a second entry on either side, so a retry inside the same
// transaction cannot unbalance the ledger.
func (t *Tx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	const query = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, $4)`

	for _, entry := range entries {
		if _, err := t.tx.Exec(ctx, query,
			entry.TransferID, entry.WalletID, entry.Type, entry.Amount); err != nil {
			return fmt.Errorf("insert %s entry for transfer %s: %w",
				entry.Type, entry.TransferID, asWalletBusy(err))
		}
	}

	return nil
}
