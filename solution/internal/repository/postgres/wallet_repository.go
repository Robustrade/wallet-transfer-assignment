package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/heisenberglit/wallet-transfer-assignment/internal/domain"
)

// WalletRepository is the pgx-backed implementation of repository.WalletRepository.
type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	const query = `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	return scanWallet(r.pool.QueryRow(ctx, query, id))
}

func scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var wallet domain.Wallet
	if err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}
