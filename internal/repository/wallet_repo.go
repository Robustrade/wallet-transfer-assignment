package repository

import (
	"context"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
)

type WalletRepo struct{}

func NewWalletRepo() *WalletRepo {
	return &WalletRepo{}
}

// LockAndGet locks both wallets in a single query, ordered by id, so any
// two concurrent transfers touching the same pair of wallets always
// acquire the row locks in the same order and cannot deadlock. The
// returned map has one entry per wallet that actually exists; a caller
// asking for two ids and getting fewer than two entries back knows exactly
// which wallet(s) are missing.
func (r *WalletRepo) LockAndGet(ctx context.Context, q Querier, walletIDs ...string) (map[string]*domain.Wallet, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = ANY($1)
		ORDER BY id
		FOR UPDATE
	`, walletIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*domain.Wallet, len(walletIDs))
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		result[w.ID] = &w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *WalletRepo) UpdateBalance(ctx context.Context, q Querier, walletID string, newBalance int64) error {
	_, err := q.ExecContext(ctx, `
		UPDATE wallets SET balance = $2, updated_at = now() WHERE id = $1
	`, walletID, newBalance)
	return err
}
