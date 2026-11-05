// Package repository runs SQL. It holds no workflow decisions: every method takes a
// db.Querier, so the service layer decides which calls share a transaction.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// WalletRepository persists wallets.
type WalletRepository struct{}

const walletColumns = `id, balance, created_at, updated_at`

func scanWallet(row interface{ Scan(...any) error }) (domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	return w, err
}

// Create inserts a wallet. It returns (nil, nil) if the id already exists.
func (WalletRepository) Create(ctx context.Context, q db.Querier, id string, balance int64) (*domain.Wallet, error) {
	w, err := scanWallet(q.QueryRowContext(ctx,
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)
		 ON CONFLICT (id) DO NOTHING
		 RETURNING `+walletColumns, id, balance))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("insert wallet: %w", err)
	}
	return &w, nil
}

// FindByID returns (nil, nil) if the wallet does not exist.
func (WalletRepository) FindByID(ctx context.Context, q db.Querier, id string) (*domain.Wallet, error) {
	w, err := scanWallet(q.QueryRowContext(ctx,
		`SELECT `+walletColumns+` FROM wallets WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find wallet: %w", err)
	}
	return &w, nil
}

// LockByIDs takes a row lock on every wallet, always in id order.
// A fixed lock order means two transfers touching the same wallets can never deadlock.
//
// FOR NO KEY UPDATE (not FOR UPDATE): it still blocks other transfers from changing
// the balance, but it does not conflict with the FOR KEY SHARE locks that Postgres takes
// for foreign-key checks when another request inserts a transfer row. With FOR UPDATE,
// such an insert (B->A, key-share on B then A) could deadlock with a transfer that holds A.
// Must be called inside a transaction.
func (WalletRepository) LockByIDs(ctx context.Context, q db.Querier, ids []string) ([]domain.Wallet, error) {
	return queryWallets(ctx, q,
		`SELECT `+walletColumns+` FROM wallets WHERE id = ANY($1) ORDER BY id FOR NO KEY UPDATE`,
		pq.Array(ids))
}

// FindByIDs returns the wallets that exist among ids.
func (WalletRepository) FindByIDs(ctx context.Context, q db.Querier, ids []string) ([]domain.Wallet, error) {
	return queryWallets(ctx, q,
		`SELECT `+walletColumns+` FROM wallets WHERE id = ANY($1) ORDER BY id`, pq.Array(ids))
}

func queryWallets(ctx context.Context, q db.Querier, query string, args ...any) ([]domain.Wallet, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query wallets: %w", err)
	}
	defer rows.Close()

	var wallets []domain.Wallet
	for rows.Next() {
		w, err := scanWallet(rows)
		if err != nil {
			return nil, fmt.Errorf("scan wallet: %w", err)
		}
		wallets = append(wallets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wallets: %w", err)
	}
	return wallets, nil
}

// AdjustBalance adds delta (may be negative). The DB CHECK stops it going below zero.
func (WalletRepository) AdjustBalance(ctx context.Context, q db.Querier, id string, delta int64) error {
	if _, err := q.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + $2, updated_at = now() WHERE id = $1`,
		id, delta); err != nil {
		return fmt.Errorf("adjust balance of %s: %w", id, err)
	}
	return nil
}
