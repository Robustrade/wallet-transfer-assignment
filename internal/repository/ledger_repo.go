package repository

import (
	"context"
	"fmt"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
)

// ledgerEntryPerTypeConstraint is the named UNIQUE constraint guaranteeing at
// most one DEBIT and one CREDIT row per transfer.
const ledgerEntryPerTypeConstraint = "unique_entry_per_transfer_type"

// LedgerRepository writes the ledger_entries table.
type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository { return &LedgerRepository{} }

// InsertEntry appends one ledger entry. A second entry of the same type for
// the same transfer violates UNIQUE(transfer_id, type); that is surfaced as
// domain.ErrDuplicateIdempotencyKey's ledger analogue via a wrapped error so
// the caller's transaction rolls back.
func (r *LedgerRepository) InsertEntry(ctx context.Context, db DBTX, e domain.LedgerEntry) error {
	const q = `INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
	           VALUES ($1, $2, $3, $4)`
	_, err := db.Exec(ctx, q, e.TransferID, e.WalletID, e.Type, e.Amount)
	if err != nil {
		if isUniqueViolation(err, ledgerEntryPerTypeConstraint) {
			return fmt.Errorf("duplicate ledger entry for transfer %s type %s: %w",
				e.TransferID, e.Type, err)
		}
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}
