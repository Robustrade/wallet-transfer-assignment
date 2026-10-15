package repository

import (
	"context"
	"fmt"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
)

// LedgerRepository handles persistence of ledger entries. It performs no
// validation of double-entry balance rules — that invariant is enforced by
// the service layer always writing a matching DEBIT/CREDIT pair together.
type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository {
	return &LedgerRepository{}
}

// InsertEntry writes a single ledger row.
func (r *LedgerRepository) InsertEntry(ctx context.Context, tx DBTX, e domain.LedgerEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount)
		VALUES ($1, $2, $3, $4)
	`, e.TransferID, e.WalletID, e.EntryType, e.Amount)
	if err != nil {
		return fmt.Errorf("inserting ledger entry: %w", err)
	}
	return nil
}

// InsertDoubleEntry writes the DEBIT + CREDIT pair for a transfer in one
// call, guaranteeing a transfer never produces an unbalanced ledger.
func (r *LedgerRepository) InsertDoubleEntry(ctx context.Context, tx DBTX, transferID, fromWalletID, toWalletID string, amount int64) error {
	if err := r.InsertEntry(ctx, tx, domain.LedgerEntry{
		TransferID: transferID,
		WalletID:   fromWalletID,
		EntryType:  domain.EntryTypeDebit,
		Amount:     amount,
	}); err != nil {
		return err
	}
	return r.InsertEntry(ctx, tx, domain.LedgerEntry{
		TransferID: transferID,
		WalletID:   toWalletID,
		EntryType:  domain.EntryTypeCredit,
		Amount:     amount,
	})
}

// ListByTransferID returns all ledger entries for a transfer, used by tests
// and potential history endpoints to verify ledger correctness.
func (r *LedgerRepository) ListByTransferID(ctx context.Context, tx DBTX, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, transfer_id, wallet_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY created_at
	`, transferID)
	if err != nil {
		return nil, fmt.Errorf("listing ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
