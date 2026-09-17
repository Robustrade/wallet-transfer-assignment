// Package repository provides database access for domain entities.
package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/shubh/wallet-service/internal/db"
	"github.com/shubh/wallet-service/internal/domain"
)

// WalletRepository handles wallet persistence operations.
type WalletRepository struct {
	db *sql.DB
}

// NewWalletRepository creates a new WalletRepository.
func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

// GetByID retrieves a wallet by its ID.
func (r *WalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	w := &domain.Wallet{}
	err := r.db.QueryRowContext(ctx,
		`SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`,
		id,
	).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet %s: %w", id, err)
	}
	return w, nil
}

// LockWalletsForTransfer acquires row-level locks on both wallets within a
// transaction. Wallets are locked in deterministic ID order to prevent deadlocks.
func (r *WalletRepository) LockWalletsForTransfer(ctx context.Context, walletID1, walletID2 string) (*domain.Wallet, *domain.Wallet, error) {
	dbtx := db.GetTxFromContext(ctx, r.db)

	// Sort IDs to always lock in consistent order — prevents deadlocks
	ids := []string{walletID1, walletID2}
	sort.Strings(ids)

	wallets := make(map[string]*domain.Wallet)
	rows, err := dbtx.QueryContext(ctx,
		`SELECT id, balance, created_at, updated_at
		 FROM wallets
		 WHERE id = ANY($1)
		 ORDER BY id
		 FOR UPDATE`,
		"{"+ids[0]+","+ids[1]+"}",
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lock wallets: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		w := &domain.Wallet{}
		if err := rows.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("failed to scan wallet: %w", err)
		}
		wallets[w.ID] = w
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("rows error: %w", err)
	}

	from, ok := wallets[walletID1]
	if !ok {
		return nil, nil, fmt.Errorf("wallet %s: %w", walletID1, domain.ErrWalletNotFound)
	}
	to, ok := wallets[walletID2]
	if !ok {
		return nil, nil, fmt.Errorf("wallet %s: %w", walletID2, domain.ErrWalletNotFound)
	}

	return from, to, nil
}

// UpdateBalance sets the balance and updated_at timestamp for a wallet within a transaction.
func (r *WalletRepository) UpdateBalance(ctx context.Context, walletID string, newBalance int64) error {
	dbtx := db.GetTxFromContext(ctx, r.db)
	_, err := dbtx.ExecContext(ctx,
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`,
		newBalance, walletID,
	)
	if err != nil {
		return fmt.Errorf("failed to update wallet %s balance: %w", walletID, err)
	}
	return nil
}
