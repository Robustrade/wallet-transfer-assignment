package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// walletRepo is the PostgreSQL implementation of WalletRepository.
type walletRepo struct {
	log *slog.Logger
}

// NewWalletRepo returns a WalletRepository backed by PostgreSQL.
func NewWalletRepo(log *slog.Logger) WalletRepository {
	return &walletRepo{log: log}
}

func (r *walletRepo) Create(ctx context.Context, q Querier, w domain.Wallet) error {
	const query = `
		INSERT INTO wallets (id, balance, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := q.Exec(ctx, query, w.ID, int64(w.Balance), w.Version, w.CreatedAt, w.UpdatedAt); err != nil {
		r.log.Error("wallet create failed", "layer", "repo", "error", err, "walletID", w.ID)

		return fmt.Errorf("repo: create wallet: %w", err)
	}

	return nil
}

func (r *walletRepo) GetByID(ctx context.Context, q Querier, id uuid.UUID) (domain.Wallet, error) {
	const query = `
		SELECT id, balance, version, created_at, updated_at
		FROM wallets
		WHERE id = $1`

	return r.scanWallet(q.QueryRow(ctx, query, id))
}

func (r *walletRepo) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id uuid.UUID) (domain.Wallet, error) {
	const query = `
		SELECT id, balance, version, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE`

	return r.scanWallet(tx.QueryRow(ctx, query, id))
}

func (r *walletRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, id uuid.UUID, newBalance domain.Amount) error {
	const query = `
		UPDATE wallets
		SET balance = $1, version = version + 1, updated_at = NOW()
		WHERE id = $2`

	tag, err := tx.Exec(ctx, query, int64(newBalance), id)
	if err != nil {
		r.log.Error("wallet update balance failed", "layer", "repo", "error", err, "walletID", id)

		return fmt.Errorf("repo: update wallet balance: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("repo: update wallet balance: %w", domain.ErrWalletNotFound)
	}

	return nil
}

func (r *walletRepo) scanWallet(row pgx.Row) (domain.Wallet, error) {
	var w domain.Wallet
	var balance int64

	err := row.Scan(&w.ID, &balance, &w.Version, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}

		return domain.Wallet{}, fmt.Errorf("repo: scan wallet: %w", err)
	}

	w.Balance = domain.Amount(balance)

	return w, nil
}
