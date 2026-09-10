package repository

import (
	"context"
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// walletRepo implements WalletRepository using PostgreSQL.
type walletRepo struct{}

// NewWalletRepository creates a new PostgreSQL wallet repository.
func NewWalletRepository() WalletRepository {
	return &walletRepo{}
}

func (r *walletRepo) Create(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO wallets (id, balance, currency, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		wallet.ID, wallet.Balance, wallet.Currency, wallet.CreatedAt, wallet.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrDuplicateWallet
		}
		return err
	}
	return nil
}

func (r *walletRepo) GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error) {
	return r.queryWallet(ctx, tx, id, false)
}

func (r *walletRepo) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error) {
	return r.queryWallet(ctx, tx, id, true)
}

func (r *walletRepo) queryWallet(ctx context.Context, tx pgx.Tx, id string, forUpdate bool) (*domain.Wallet, error) {
	query := `SELECT id, balance, currency, created_at, updated_at FROM wallets WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	w := &domain.Wallet{}
	err := tx.QueryRow(ctx, query, id).Scan(
		&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return w, nil
}

func (r *walletRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, id string, newBalance int64) error {
	cmdTag, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`,
		newBalance, id,
	)
	if err != nil {
		return err
	}
	if cmdTag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}
