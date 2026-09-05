package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallet-transfer/internal/domain"
)

type WalletRepository struct {
	tx *TxManager
}

func NewWalletRepository(tx *TxManager) *WalletRepository { return &WalletRepository{tx: tx} }

func (r *WalletRepository) Get(ctx context.Context, id string) (domain.Wallet, error) {
	const query = `
		SELECT id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1`

	var w domain.Wallet
	err := r.tx.executorFrom(ctx).QueryRowContext(ctx, query, id).
		Scan(&w.ID, &w.Currency, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	case err != nil:
		return domain.Wallet{}, fmt.Errorf("get wallet %s: %w", id, translateError(err))
	}
	return w, nil
}

func (r *WalletRepository) LockPair(ctx context.Context, firstID, secondID string) (domain.Wallet, domain.Wallet, error) {
	const query = `
		SELECT id, currency, balance, created_at, updated_at
		FROM wallets
		WHERE id IN ($1, $2)
		ORDER BY id
		FOR UPDATE`

	rows, err := r.tx.executorFrom(ctx).QueryContext(ctx, query, firstID, secondID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallets: %w", translateError(err))
	}
	defer func() { _ = rows.Close() }()

	found := make(map[string]domain.Wallet, 2)
	for rows.Next() {
		var w domain.Wallet
		if err := rows.Scan(&w.ID, &w.Currency, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("scan locked wallet: %w", translateError(err))
		}
		found[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallets: %w", translateError(err))
	}

	first, ok := found[firstID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, firstID)
	}
	second, ok := found[secondID]
	if !ok {
		return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, secondID)
	}
	return first, second, nil
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, id string, balance domain.Amount) error {
	const query = `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1`

	result, err := r.tx.executorFrom(ctx).ExecContext(ctx, query, id, int64(balance))
	if err != nil {
		return fmt.Errorf("update balance for %s: %w", id, translateError(err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update balance for %s: %w", id, translateError(err))
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	return nil
}
