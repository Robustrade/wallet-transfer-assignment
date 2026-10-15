package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// WalletRepository handles persistence for wallets. It contains no workflow
// decisions (e.g. "is this transfer allowed") — only data access.
type WalletRepository struct{}

func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

// Get fetches a wallet without taking a row lock. Suitable for read-only
// lookups such as a balance API.
func (r *WalletRepository) Get(ctx context.Context, tx DBTX, id string) (domain.Wallet, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id)
	return scanWallet(row)
}

// LockForUpdate fetches a wallet and takes a row-level lock
// (SELECT ... FOR UPDATE) so concurrent transfers touching the same wallet
// serialize on this row until the enclosing transaction commits or rolls
// back. Must be called within a transaction.
func (r *WalletRepository) LockForUpdate(ctx context.Context, tx DBTX, id string) (domain.Wallet, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, id)
	return scanWallet(row)
}

// LockTwoForUpdate locks both wallets involved in a transfer in a stable,
// deterministic order (sorted by wallet id) regardless of which one is the
// debit side and which is the credit side. Locking in a consistent order
// across all transfers prevents deadlocks that would otherwise occur when
// two concurrent transfers move money between the same pair of wallets in
// opposite directions.
func (r *WalletRepository) LockTwoForUpdate(ctx context.Context, tx DBTX, idA, idB string) (a, b domain.Wallet, err error) {
	ids := []string{idA, idB}
	sort.Strings(ids)

	locked := make(map[string]domain.Wallet, 2)
	for _, id := range ids {
		w, err := r.LockForUpdate(ctx, tx, id)
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, err
		}
		locked[id] = w
	}
	return locked[idA], locked[idB], nil
}

// UpdateBalance sets a wallet's balance to newBalance.
func (r *WalletRepository) UpdateBalance(ctx context.Context, tx DBTX, id string, newBalance int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1
	`, id, newBalance)
	if err != nil {
		return fmt.Errorf("updating wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

// Create inserts a new wallet with the given starting balance. Intended for
// seeding/test setup rather than the core transfer workflow.
func (r *WalletRepository) Create(ctx context.Context, tx DBTX, id string, balance int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
	`, id, balance)
	if err != nil {
		return fmt.Errorf("creating wallet: %w", err)
	}
	return nil
}

func scanWallet(row pgx.Row) (domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, fmt.Errorf("scanning wallet: %w", err)
	}
	return w, nil
}
