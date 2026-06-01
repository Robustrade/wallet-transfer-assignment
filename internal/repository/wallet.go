package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/example/wallet-transfer-assignment/internal/domain"
)

type WalletRepo struct {
	db *sql.DB
}

func NewWalletRepo(db *sql.DB) *WalletRepo {
	return &WalletRepo{db: db}
}

func (r *WalletRepo) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	query := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	if tx != nil {
		query = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`
	}

	executor := r.queryExecutor(tx)
	w := &domain.Wallet{}
	err := executor.QueryRowContext(ctx, query, id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

func (r *WalletRepo) Create(ctx context.Context, id string, balance int64) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`,
		id, balance,
	)
	if err != nil {
		return fmt.Errorf("create wallet: %w", err)
	}
	return nil
}

func (r *WalletRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, id string, delta int64) error {
	if tx == nil {
		return fmt.Errorf("update balance requires a transaction")
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`,
		delta, id,
	)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}
	return nil
}

func (r *WalletRepo) queryExecutor(tx *sql.Tx) interface {
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
} {
	if tx != nil {
		return tx
	}
	return r.db
}
