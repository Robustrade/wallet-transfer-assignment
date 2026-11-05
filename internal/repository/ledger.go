package repository

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// LedgerRepository persists ledger entries. The table is append-only.
type LedgerRepository struct{}

// InsertPair writes the DEBIT and CREDIT of one transfer in a single statement.
func (LedgerRepository) InsertPair(ctx context.Context, q db.Querier, t domain.Transfer) error {
	if _, err := q.ExecContext(ctx,
		`INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount)
		 VALUES ($1, $2, 'DEBIT', $4), ($1, $3, 'CREDIT', $4)`,
		t.ID, t.FromWalletID, t.ToWalletID, t.Amount); err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}
	return nil
}

// FindByWalletID returns the newest entries of a wallet first.
func (LedgerRepository) FindByWalletID(ctx context.Context, q db.Querier, walletID string, limit int) ([]domain.LedgerEntry, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id, transfer_id, wallet_id, type, amount, created_at
		   FROM ledger_entries WHERE wallet_id = $1 ORDER BY id DESC LIMIT $2`, walletID, limit)
	if err != nil {
		return nil, fmt.Errorf("query ledger: %w", err)
	}
	defer rows.Close()

	entries := []domain.LedgerEntry{}
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.TransferID, &e.WalletID, &e.Type, &e.Amount, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger: %w", err)
	}
	return entries, nil
}
