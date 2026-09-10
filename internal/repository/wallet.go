package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

func (r *DBRepository) GetWalletForUpdate(
	ctx context.Context,
	db *sql.Tx,
	walletID uuid.UUID,
) (*domain.Wallet, error) {
	const query = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var wallet domain.Wallet

	err := db.QueryRowContext(ctx, query, walletID).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}

		return nil, fmt.Errorf("get wallet for update: %w", err)
	}

	return &wallet, nil
}

func (r *DBRepository) UpdateWalletBalance(
	ctx context.Context,
	db *sql.Tx,
	walletID uuid.UUID,
	balance int64,
) error {
	const query = `
		UPDATE wallets
		SET balance = $1,
		    updated_at = NOW()
		WHERE id = $2
	`

	result, err := db.ExecContext(ctx, query, balance, walletID)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get updated wallet rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return domain.ErrWalletNotFound
	}

	return nil
}
