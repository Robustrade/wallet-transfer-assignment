package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WalletRepository struct{}

func NewWalletRepository() *WalletRepository { return &WalletRepository{} }

// GetForUpdate reads and locks a wallet row until the surrounding transaction
// commits or rolls back. The caller must use this inside a transaction when
// it intends to make a balance decision/update.
func (r *WalletRepository) GetForUpdate(ctx context.Context, db DBTX, id uuid.UUID) (domain.Wallet, error) {
	const q = `
        SELECT id, balance, created_at, updated_at
        FROM wallets
        WHERE id = $1
        FOR UPDATE`

	var w domain.Wallet
	err := db.QueryRow(ctx, q, id).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("get wallet for update: %w", err)
	}
	return w, nil
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, db DBTX, id uuid.UUID, balance int64) error {
	const q = `
        UPDATE wallets
        SET balance = $2, updated_at = NOW()
        WHERE id = $1`

	tag, err := db.Exec(ctx, q, id, balance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: %s", domain.ErrWalletNotFound, id)
	}
	return nil
}
