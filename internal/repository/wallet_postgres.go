package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
)

type PostgresWalletRepository struct {
	db *sql.DB
}

func NewPostgresWalletRepository(db *sql.DB) *PostgresWalletRepository {
	return &PostgresWalletRepository{db: db}
}

func (r *PostgresWalletRepository) GetWallet(ctx context.Context, id uuid.UUID) (*domain.Wallet, error) {
	var (
		w           domain.Wallet
		balanceText string
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, balance, created_at, updated_at FROM wallets WHERE id = $1`,
		id,
	).Scan(&w.ID, &w.Name, &balanceText, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("querying wallet: %w", err)
	}
	cents, err := domain.NumericToCents(balanceText)
	if err != nil {
		return nil, fmt.Errorf("parsing wallet balance: %w", err)
	}
	w.Balance = cents
	return &w, nil
}

func (r *PostgresWalletRepository) CreateWallet(ctx context.Context, name string, initialBalanceCents int64) (*domain.Wallet, error) {
	var (
		w           domain.Wallet
		balanceText string
	)
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO wallets (name, balance) VALUES ($1, $2)
		 RETURNING id, name, balance, created_at, updated_at`,
		name, domain.CentsToNumeric(initialBalanceCents),
	).Scan(&w.ID, &w.Name, &balanceText, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("creating wallet: %w", err)
	}
	cents, err := domain.NumericToCents(balanceText)
	if err != nil {
		return nil, fmt.Errorf("parsing wallet balance: %w", err)
	}
	w.Balance = cents
	return &w, nil
}
