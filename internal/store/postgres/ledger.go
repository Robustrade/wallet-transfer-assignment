package postgres

import (
	"context"
	"fmt"

	"wallet/internal/constants"
	"wallet/internal/domain"
)

// InsertLedgerEntries implements service.Tx.
func (t *txStore) InsertLedgerEntries(ctx context.Context, transferID, fromWalletID, toWalletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, constants.InsertLedgerEntriesSQL, transferID, fromWalletID, toWalletID, amount)
	if err != nil {
		return mapError(fmt.Errorf("insert ledger entries: %w", err))
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("insert ledger entries: inserted %d rows, want exactly 2", tag.RowsAffected())
	}
	return nil
}

// ListWalletLedger implements service.Store.
func (s *Store) ListWalletLedger(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error) {
	ctx, cancel := boundedPoolCtx(ctx)
	defer cancel()

	var exists bool
	if err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM wallets WHERE id = $1)", walletID).Scan(&exists); err != nil {
		return nil, mapError(fmt.Errorf("check wallet for ledger: %w", err))
	}
	if !exists {
		return nil, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, walletID)
	}

	rows, err := s.pool.Query(ctx, constants.ListWalletLedgerSQL, walletID, limit)
	if err != nil {
		return nil, mapError(fmt.Errorf("list wallet ledger: %w", err))
	}
	defer rows.Close()

	entries := make([]domain.LedgerEntry, 0, limit)
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(fmt.Errorf("list wallet ledger: %w", err))
	}
	return entries, nil
}