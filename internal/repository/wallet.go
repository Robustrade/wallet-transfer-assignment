package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (db *DB) CreateWallet(ctx context.Context, balance int64) (*domain.Wallet, error) {
	row := db.Pool.QueryRow(ctx, `
		INSERT INTO wallets (balance)
		VALUES ($1)
		RETURNING id, balance, created_at, updated_at
	`, balance)

	wallet, err := scanWallet(row)
	if err != nil {
		return nil, fmt.Errorf("create wallet: %w", err)
	}
	return wallet, nil
}

func (db *DB) GetWallet(ctx context.Context, walletID uuid.UUID) (*domain.Wallet, error) {
	row := db.Pool.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, walletID)

	wallet, err := scanWallet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}
	return wallet, nil
}

func GetWalletForUpdate(ctx context.Context, tx pgx.Tx, walletID uuid.UUID) (*domain.Wallet, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, walletID)

	wallet, err := scanWallet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet for update: %w", err)
	}
	return wallet, nil
}

func UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID uuid.UUID, delta int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $1, updated_at = now()
		WHERE id = $2
	`, delta, walletID)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanWallet(row scannable) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &wallet, nil
}
