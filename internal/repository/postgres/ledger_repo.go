package postgres

import (
	"context"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// ledgerRepo implements repository.LedgerRepository against PostgreSQL.
type ledgerRepo struct {
	q querier
}

// InsertPair writes the balanced debit/credit pair in a single batched
// statement. The UNIQUE(transfer_id, type) constraint guarantees a transfer can
// never accumulate duplicate entries even if processing is retried.
func (r *ledgerRepo) InsertPair(ctx context.Context, debit, credit *domain.LedgerEntry) error {
	const query = `
		INSERT INTO ledger_entries (transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, $4, $5), ($6, $7, $8, $9, $10)`
	_, err := r.q.Exec(ctx, query,
		debit.TransferID, debit.WalletID, debit.Type, debit.Amount, debit.CreatedAt,
		credit.TransferID, credit.WalletID, credit.Type, credit.Amount, credit.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert ledger pair: %w", err)
	}
	return nil
}

// ListByWallet returns the ledger history for a wallet, newest first.
func (r *ledgerRepo) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	const query = `
		SELECT id, transfer_id, wallet_id, type, amount, created_at
		FROM ledger_entries
		WHERE wallet_id = $1
		ORDER BY id DESC`
	rows, err := r.q.Query(ctx, query, walletID)
	if err != nil {
		return nil, fmt.Errorf("query ledger: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
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
