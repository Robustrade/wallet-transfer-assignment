package repository

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

const ledgerColumns = "id, transfer_id, wallet_id, entry_type, amount, created_at"

func (c *queries) InsertLedgerPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	for _, entry := range []domain.LedgerEntry{debit, credit} {
		_, err := c.exec(ctx,
			`INSERT INTO ledger_entries (`+ledgerColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
			entry.ID, entry.TransferID, entry.WalletID, entry.EntryType, entry.Amount, entry.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert %s entry for transfer %s: %w", entry.EntryType, entry.TransferID, err)
		}
	}
	return nil
}

func (c *queries) ListLedgerEntriesByTransfer(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := c.query(ctx,
		`SELECT `+ledgerColumns+` FROM ledger_entries
		 WHERE transfer_id = ? ORDER BY CASE entry_type WHEN 'DEBIT' THEN 0 ELSE 1 END`, transferID)
	if err != nil {
		return nil, fmt.Errorf("select ledger entries for transfer %s: %w", transferID, err)
	}
	defer func() { _ = rows.Close() }()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.EntryType, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries for transfer %s: %w", transferID, err)
	}
	return entries, nil
}
