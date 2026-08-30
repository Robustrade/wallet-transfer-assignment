package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type PostgresWalletRepository struct{}

func NewPostgresWalletRepository() *PostgresWalletRepository {
	return &PostgresWalletRepository{}
}

func (r *PostgresWalletRepository) GetByID(
	ctx context.Context,
	tx pgx.Tx,
	id string,
) (*domain.Wallet, error) {
	const query = `
		SELECT id, balance, created_at
		FROM wallets
		WHERE id = $1
	`

	var wallet domain.Wallet

	err := tx.QueryRow(ctx, query, id).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrWalletNotFound, id)
		}

		return nil, fmt.Errorf("get wallet: %w", err)
	}

	return &wallet, nil
}

func (r *PostgresWalletRepository) GetByIDForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	id string,
) (*domain.Wallet, error) {
	const query = `
		SELECT id, balance, created_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var wallet domain.Wallet

	err := tx.QueryRow(ctx, query, id).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrWalletNotFound, id)
		}

		return nil, fmt.Errorf("get wallet for update: %w", err)
	}

	return &wallet, nil
}

func (r *PostgresWalletRepository) UpdateBalance(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	balance int64,
) error {
	const query = `
		UPDATE wallets
		SET balance = $1
		WHERE id = $2
	`

	result, err := tx.Exec(ctx, query, balance, id)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	if result.RowsAffected() != 1 {
		return fmt.Errorf("%w: %s", ErrWalletNotFound, id)
	}

	return nil
}
