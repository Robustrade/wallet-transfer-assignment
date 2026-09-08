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
	GetWalletsForUpdate(ctx context.Context, id1, id2 string) ([]domain.Wallet, error)
	UpdateWalletBalance(ctx context.Context, id string, balance int64) error
}

type PostgresWalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *PostgresWalletRepository {
	return &PostgresWalletRepository{
		pool: pool,
	}
}

func (r *PostgresWalletRepository) q(ctx context.Context) *db.Queries {
	return db.New(getDBTX(ctx, r.pool))
}

func (r *PostgresWalletRepository) CreateWallet(ctx context.Context, id string, balance int64) (*domain.Wallet, error) {
	row, err := r.q(ctx).CreateWallet(ctx, db.CreateWalletParams{
		ID:      id,
		Balance: balance,
	})
	if err != nil {
		return nil, err
	}
	return toDomainWallet(row), nil
}

func (r *PostgresWalletRepository) GetWallet(ctx context.Context, id string) (*domain.Wallet, error) {
	row, err := r.q(ctx).GetWallet(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return toDomainWallet(row), nil
}

func (r *PostgresWalletRepository) GetWalletsForUpdate(ctx context.Context, id1, id2 string) ([]domain.Wallet, error) {
	rows, err := r.q(ctx).GetWalletsForUpdate(ctx, db.GetWalletsForUpdateParams{
		ID:   id1,
		ID_2: id2,
	})
	if err != nil {
		return nil, err
	}
	wallets := make([]domain.Wallet, len(rows))
	for i, row := range rows {
		wallets[i] = *toDomainWallet(row)
	}
	return wallets, nil
}

func (r *PostgresWalletRepository) UpdateWalletBalance(ctx context.Context, id string, balance int64) error {
	return r.q(ctx).UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID:      id,
		Balance: balance,
	})
}

func toDomainWallet(w db.Wallet) *domain.Wallet {
	return &domain.Wallet{
		ID:        w.ID,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt.Time,
		UpdatedAt: w.UpdatedAt.Time,
	}
}
