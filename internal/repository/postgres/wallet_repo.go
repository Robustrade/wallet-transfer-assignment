package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallettransfer/internal/domain"
)

// WalletRepo is the PostgreSQL implementation of service.WalletRepo.
type WalletRepo struct {
	db *sql.DB
}

func NewWalletRepo(db *sql.DB) *WalletRepo { return &WalletRepo{db: db} }

// LockForUpdate reads a wallet's current balance and takes a row-level
// exclusive lock on it for the lifetime of tx. Any other transaction trying
// to lock or update the same row will block until tx commits or rolls back.
// This is the mechanism that makes concurrent transfers touching the same
// wallet serialize correctly instead of racing.
func (r *WalletRepo) LockForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (domain.Wallet, error) {
	const q = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`
	var w domain.Wallet
	err := tx.QueryRowContext(ctx, q, walletID).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("lock wallet %s: %w", walletID, err)
	}
	return w, nil
}

func (r *WalletRepo) UpdateBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error {
	const q = `UPDATE wallets SET balance = $1, updated_at = now() WHERE id = $2`
	res, err := tx.ExecContext(ctx, q, newBalance, walletID)
	if err != nil {
		return fmt.Errorf("update balance for %s: %w", walletID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *WalletRepo) Get(ctx context.Context, walletID string) (domain.Wallet, error) {
	const q = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	var w domain.Wallet
	err := r.db.QueryRowContext(ctx, q, walletID).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet %s: %w", walletID, err)
	}
	return w, nil
}

func (r *WalletRepo) Create(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error) {
	const q = `
		INSERT INTO wallets (id, balance, created_at, updated_at)
		VALUES ($1, $2, now(), now())
		RETURNING id, balance, created_at, updated_at`
	var w domain.Wallet
	err := r.db.QueryRowContext(ctx, q, walletID, openingBalance).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("create wallet %s: %w", walletID, err)
	}
	return w, nil
}
