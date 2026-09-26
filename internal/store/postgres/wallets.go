package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wallet/internal/constants"
	"wallet/internal/domain"
)

// LockWallets implements service.Tx.
func (t *txStore) LockWallets(ctx context.Context, ids []string) ([]domain.Wallet, error) {
	rows, err := t.tx.Query(ctx, constants.LockWalletsSQL, ids)
	if err != nil {
		return nil, mapError(fmt.Errorf("lock wallets: %w", err))
	}
	defer rows.Close()

	var wallets []domain.Wallet
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan locked wallet: %w", err)
		}
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(fmt.Errorf("lock wallets: %w", err))
	}
	return wallets, nil
}

// DebitWallet implements service.Tx.
func (t *txStore) DebitWallet(ctx context.Context, walletID string, amount int64) (bool, error) {
	tag, err := t.tx.Exec(ctx, constants.DebitWalletSQL, walletID, amount)
	if err != nil {
		return false, mapError(fmt.Errorf("debit wallet: %w", err))
	}
	return tag.RowsAffected() == 1, nil
}

// CreditWallet implements service.Tx.
func (t *txStore) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	tag, err := t.tx.Exec(ctx, constants.CreditWalletSQL, walletID, amount)
	if err != nil {
		return mapError(fmt.Errorf("credit wallet: %w", err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("credit wallet: updated %d rows, want exactly 1", tag.RowsAffected())
	}
	return nil
}

// GetWallet implements service.Store.
func (s *Store) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	ctx, cancel := boundedPoolCtx(ctx)
	defer cancel()
	return getWallet(ctx, s.pool, id)
}

func getWallet(ctx context.Context, q querier, id string) (domain.Wallet, error) {
	var w domain.Wallet
	err := q.QueryRow(ctx, constants.GetWalletSQL, id).
		Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	if err != nil {
		return domain.Wallet{}, mapError(fmt.Errorf("get wallet: %w", err))
	}
	return w, nil
}
