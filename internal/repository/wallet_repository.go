package repository

import (
	"context"
	"database/sql"
	"errors"

	"wallet-transfer-assignment/internal/domain"
)

var ErrWalletNotFound = errors.New("wallet not found")

type walletRepository struct{}

func NewWalletRepository() WalletRepository {
	return &walletRepository{}
}

func (r *walletRepository) GetWalletForUpdate(ctx context.Context, tx *sql.Tx, walletId string) (*domain.Wallet, error) {
	var wallet domain.Wallet

	const query = `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	err := tx.QueryRowContext(ctx, query, walletId).Scan(
		&wallet.Id,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWalletNotFound
	}

	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

func (r *walletRepository) UpdateBalance(ctx context.Context, tx *sql.Tx, walletId string, balance int64) error {
	const query = `
		UPDATE wallets
		SET balance = $1,
		    updated_at = NOW()
		WHERE id = $2
	`

	result, err := tx.ExecContext(ctx, query, balance, walletId)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrWalletNotFound
	}

	return nil
}
