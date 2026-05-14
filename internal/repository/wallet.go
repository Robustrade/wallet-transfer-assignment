package repository

import (
	"context"
	"errors"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/domain"

	"github.com/jackc/pgx/v5"
)

// WalletRepository handles all persistence operations for wallets.
type WalletRepository struct{}

func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

// Create inserts a new wallet record.
func (r *WalletRepository) Create(ctx context.Context, dbtx db.DBTX, w *domain.Wallet) error {
	const q = `INSERT INTO wallets (id, balance) VALUES ($1, $2)`
	_, err := dbtx.Exec(ctx, q, w.ID, w.Balance)
	return err
}

// GetByID fetches a wallet by ID without acquiring a row lock.
func (r *WalletRepository) GetByID(ctx context.Context, dbtx db.DBTX, id string) (*domain.Wallet, error) {
	const q = `SELECT id, balance, created_at FROM wallets WHERE id = $1`
	return r.scanWallet(dbtx.QueryRow(ctx, q, id))
}

// GetByIDForUpdate fetches a wallet and acquires a row-level exclusive lock.
// This MUST be called within a transaction; the lock is released on commit or rollback.
// Callers should lock wallets in a consistent order (e.g. sorted by ID) to prevent deadlocks.
func (r *WalletRepository) GetByIDForUpdate(ctx context.Context, dbtx db.DBTX, id string) (*domain.Wallet, error) {
	const q = `SELECT id, balance, created_at FROM wallets WHERE id = $1 FOR UPDATE`
	return r.scanWallet(dbtx.QueryRow(ctx, q, id))
}

// UpdateBalance adds delta to a wallet's balance (use a negative delta for debits).
// The CHECK (balance >= 0) constraint in the DB provides a final safety net.
func (r *WalletRepository) UpdateBalance(ctx context.Context, dbtx db.DBTX, id string, delta int64) error {
	const q = `UPDATE wallets SET balance = balance + $1 WHERE id = $2`
	tag, err := dbtx.Exec(ctx, q, delta, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *WalletRepository) scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &w, nil
}
