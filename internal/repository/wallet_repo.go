package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/db"
)

type WalletRepository interface {
	CreateWallet(ctx context.Context, id string, balance int64) (*domain.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
}

type PostgresWalletRepository struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewWalletRepository(pool *pgxpool.Pool) *PostgresWalletRepository {
	return &PostgresWalletRepository{
		pool:    pool,
		queries: db.New(pool),
	}
}

func (r *PostgresWalletRepository) CreateWallet(ctx context.Context, id string, balance int64) (*domain.Wallet, error) {
	row, err := r.queries.CreateWallet(ctx, db.CreateWalletParams{
		ID:      id,
		Balance: balance,
	})
	if err != nil {
		return nil, err
	}
	return toDomainWallet(row), nil
}

func (r *PostgresWalletRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	row, err := r.queries.GetWallet(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return toDomainWallet(row), nil
}

func toDomainWallet(w db.Wallet) *domain.Wallet {
	return &domain.Wallet{
		ID:        w.ID,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt.Time,
		UpdatedAt: w.UpdatedAt.Time,
	}
}
