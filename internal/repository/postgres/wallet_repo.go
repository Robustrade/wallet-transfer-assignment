package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
)

type WalletRepo struct {
	db DBTX
}

func NewWalletRepo(db DBTX) *WalletRepo {
	return &WalletRepo{db: db}
}

// GetForUpdate locks the wallet row for the lifetime of the caller's
// transaction, so concurrent transfers touching the same wallet serialize on
// this read instead of racing on the subsequent balance update.
func (r *WalletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, id)

	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (r *WalletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1
	`, id, newBalance)
	return err
}

func (r *WalletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, id)

	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &w, nil
}
