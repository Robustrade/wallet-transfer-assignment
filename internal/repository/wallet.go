package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wallet-transfer/internal/domain"
)

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) Create(ctx context.Context, w *domain.Wallet) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		w.ID, w.Balance, w.CreatedAt, w.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) GetByID(ctx context.Context, exec Executor, id string) (*domain.Wallet, error) {
	row := exec.QueryRowContext(ctx,
		`SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?`,
		id,
	)
	w := &domain.Wallet{}
	err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

// DB returns the underlying database connection, useful when callers need an
// Executor outside of a transaction.
func (r *WalletRepository) DB() *sql.DB {
	return r.db
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, exec Executor, id string, newBalance int64, updatedAt time.Time) error {
	res, err := exec.ExecContext(ctx,
		`UPDATE wallets SET balance = ?, updated_at = ? WHERE id = ?`,
		newBalance, updatedAt, id,
	)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}
