package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/priiyank-xd/wallet-transfer-service/internal/domain"
)

type WalletRepository interface {
	GetWalletWithLock(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error)
	UpdateBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error
}

func (r *Repository) GetWalletWithLock(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error) {
	query := `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	var w domain.Wallet
	err := tx.QueryRow(ctx, query, walletID).Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, err
	}

	return &w, nil
}

func (r *Repository) UpdateBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error {
	query := `UPDATE wallets SET balance = balance + $2, updated_at = NOW() WHERE id = $1`

	_, err := tx.Exec(ctx, query, walletID, delta)
	return err
}
