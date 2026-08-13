package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

const uniqueViolationCode = "23505"

type WalletRepository struct {
	db *db
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{db: &db{pool: pool}}
}

func (r *WalletRepository) Create(ctx context.Context, w *domain.Wallet) error {
	row := r.db.q(ctx).QueryRow(ctx, `
		INSERT INTO wallets (id, balance, currency)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at
	`, w.ID, w.Balance, w.Currency)

	if err := row.Scan(&w.CreatedAt, &w.UpdatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return domain.ErrWalletAlreadyExists
		}
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

func (r *WalletRepository) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.scanOne(ctx, `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets WHERE id = $1
	`, id)
}

// GetForUpdate locks the wallet row for the lifetime of the caller's
// transaction. Must be called with a ctx produced by TxManager.WithinTx.
func (r *WalletRepository) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.scanOne(ctx, `
		SELECT id, balance, currency, created_at, updated_at
		FROM wallets WHERE id = $1 FOR UPDATE
	`, id)
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	tag, err := r.db.q(ctx).Exec(ctx, `
		UPDATE wallets SET balance = $1, updated_at = now() WHERE id = $2
	`, newBalance, id)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (r *WalletRepository) scanOne(ctx context.Context, query string, args ...any) (*domain.Wallet, error) {
	row := r.db.q(ctx).QueryRow(ctx, query, args...)

	var w domain.Wallet
	if err := row.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}
	return &w, nil
}
