package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

type walletRepo struct {
	db dbtx
}

func (r *walletRepo) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO wallets (id, balance, created_at, updated_at)
		VALUES ($1, $2, $3, $3)`,
		wallet.ID, wallet.Balance, wallet.CreatedAt)
	if isUniqueViolation(err) {
		return domain.ErrWalletAlreadyExists
	}
	return err
}

func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.scanWallet(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`, id)
}

func (r *walletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.scanWallet(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1 FOR UPDATE`, id)
}

func (r *walletRepo) scanWallet(ctx context.Context, query string, id string) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, query, id)

	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.WalletNotFoundError{ID: id}
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *walletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	tag, err := r.db.Exec(ctx, `UPDATE wallets SET balance = $2, updated_at = now() WHERE id = $1`, id, newBalance)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.WalletNotFoundError{ID: id}
	}
	return nil
}
