package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"Wallet_Transfer_Service/internal/domain"
)

type walletRepo struct {
	db dbtx
}

func (r *walletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	const q = `
		INSERT INTO wallets (id, balance, created_at, updated_at)
		VALUES ($1, $2, $3, $4)`
	_, err := r.db.ExecContext(ctx, q, w.ID, w.Balance, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrWalletExists
		}
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, id, "")
}

// GetForUpdate must only be called inside a transaction
func (r *walletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, id, " FOR UPDATE")
}

func (r *walletRepo) get(ctx context.Context, id, suffix string) (*domain.Wallet, error) {
	q := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1` + suffix
	w := &domain.Wallet{}
	err := r.db.QueryRowContext(ctx, q, id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

func (r *walletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	const q = `UPDATE wallets SET balance = $1, updated_at = now() WHERE id = $2`
	res, err := r.db.ExecContext(ctx, q, newBalance, id)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if n == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}
