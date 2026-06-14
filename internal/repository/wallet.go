package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/wallet-transfer-assignment/internal/domain"
)

// WalletRepository handles all persistence operations for wallets.
type WalletRepository struct {
	db DBTX
}

func NewWalletRepository(db DBTX) *WalletRepository {
	return &WalletRepository{db: db}
}

// GetByID fetches a wallet by its primary key.
func (r *WalletRepository) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	const q = `
		SELECT id, balance, version, created_at, updated_at
		FROM   wallets
		WHERE  id = $1`

	w := &domain.Wallet{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&w.ID, &w.Balance, &w.Version, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wallet.GetByID: %w", err)
	}
	return w, nil
}

// GetByIDForUpdate fetches a wallet and locks the row for the duration of
// the calling transaction (SELECT … FOR UPDATE). Must be called inside a Tx.
func (r *WalletRepository) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	const q = `
		SELECT id, balance, version, created_at, updated_at
		FROM   wallets
		WHERE  id = $1
		FOR UPDATE`

	w := &domain.Wallet{}
	err := r.db.QueryRow(ctx, q, id).Scan(
		&w.ID, &w.Balance, &w.Version, &w.CreatedAt, &w.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("wallet.GetByIDForUpdate: %w", err)
	}
	return w, nil
}

// UpdateBalance applies a delta to the wallet balance and bumps the version.
// The CHECK constraint on the wallets table (balance >= 0) acts as the last
// line of defence against a negative balance if application logic ever fails.
func (r *WalletRepository) UpdateBalance(ctx context.Context, id string, delta int64, currentVersion int64) error {
	const q = `
		UPDATE wallets
		SET    balance    = balance + $1,
		       version    = version + 1,
		       updated_at = NOW()
		WHERE  id      = $2
		AND    version = $3`

	tag, err := r.db.Exec(ctx, q, delta, id, currentVersion)
	if err != nil {
		return fmt.Errorf("wallet.UpdateBalance: %w", err)
	}
	// If no row was updated the wallet was either not found or was modified
	// concurrently — both are safety violations that must not be silenced.
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("wallet.UpdateBalance: concurrent modification detected for wallet %s", id)
	}
	return nil
}
