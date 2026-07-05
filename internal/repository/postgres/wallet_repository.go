package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
)

type WalletRepository struct{}

func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

// GetForUpdate locks the wallet row (SELECT ... FOR UPDATE) so concurrent
// transfers touching the same wallet serialize on this row.
func (r *WalletRepository) GetForUpdate(ctx context.Context, q repository.Querier, id string) (*domain.Wallet, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, id)

	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}

	return &w, nil
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, q repository.Querier, id string, newBalance int64) error {
	res, err := q.ExecContext(ctx, `
		UPDATE wallets
		SET balance = $1, updated_at = now()
		WHERE id = $2
	`, newBalance, id)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return domain.ErrWalletNotFound
	}

	return nil
}

// Get reads the wallet balance without locking — used for read-only balance queries.
func (r *WalletRepository) Get(ctx context.Context, q repository.Querier, id string) (*domain.Wallet, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id)

	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}

	return &w, nil
}
