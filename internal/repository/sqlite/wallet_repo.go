package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
)

type walletRepo struct {
	tx *sql.Tx
}

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.scanOne(ctx, "SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?", id)
}

// LockForUpdate reads the wallet within the enclosing transaction. On
// Postgres this would be `SELECT ... FOR UPDATE` to take a row lock; SQLite
// has no row-level locking, but since every write transaction is opened with
// BEGIN IMMEDIATE (see db.go) and the connection pool is capped at one
// connection, the enclosing transaction already holds exclusive write access
// to the whole database, which gives the same "no concurrent mutation of
// this wallet" guarantee.
func (r *walletRepo) LockForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.Get(ctx, id)
}

func (r *walletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64, now time.Time) error {
	res, err := r.tx.ExecContext(ctx, `UPDATE wallets SET balance = ?, updated_at = ? WHERE id = ?`, newBalance, now, id)
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

func (r *walletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	_, err := r.tx.ExecContext(ctx,
		`INSERT INTO wallets (id, balance, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		w.ID, w.Balance, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	return nil
}

func (r *walletRepo) scanOne(ctx context.Context, query string, args ...any) (*domain.Wallet, error) {
	row := r.tx.QueryRowContext(ctx, query, args...)
	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}
	return &w, nil
}
