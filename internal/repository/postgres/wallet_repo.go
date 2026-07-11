package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

// WalletRepo persists wallets.
type WalletRepo struct{}

// NewWalletRepo constructs a WalletRepo.
func NewWalletRepo() *WalletRepo {
	return &WalletRepo{}
}

// GetForUpdate locks and returns the wallets identified by walletIDs, one
// row at a time, in ascending ID order — sorted internally, unconditionally,
// so that concurrent transfers touching the same wallet pair always
// request locks in the same order and can never deadlock, regardless of
// what order the caller happened to pass walletIDs in.
//
// This deliberately issues one `SELECT ... WHERE id = $1 FOR UPDATE` per
// wallet rather than a single `WHERE id = ANY($1) ORDER BY id FOR
// UPDATE`: Postgres's row locking happens during the scan, and when a
// Sort node is needed to satisfy ORDER BY, that scan can visit rows in
// an order that doesn't match the final sorted output — so the ORDER BY
// there does not reliably control lock acquisition order. Sequential
// single-row locks make the acquisition order explicit and impossible
// for the planner to reorder.
func (r *WalletRepo) GetForUpdate(ctx context.Context, tx repository.Tx, walletIDs ...string) ([]domain.Wallet, error) {
	sort.Strings(walletIDs)

	wallets := make([]domain.Wallet, 0, len(walletIDs))
	for _, id := range walletIDs {
		row := tx.QueryRow(ctx, `
			SELECT id, balance, created_at, updated_at
			FROM wallets
			WHERE id = $1
			FOR UPDATE
		`, id)

		var w domain.Wallet
		err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock wallet %s: %w", id, err)
		}
		wallets = append(wallets, w)
	}
	return wallets, nil
}

// UpdateBalance sets walletID's balance to newBalance. It makes no
// decision about whether newBalance is valid — that's domain/service's
// call, not this repository's.
func (r *WalletRepo) UpdateBalance(ctx context.Context, tx repository.Tx, walletID string, newBalance decimal.Decimal) error {
	_, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1
	`, walletID, newBalance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	return nil
}
