package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type walletRepository struct {
	db DBTX
}

// NewWalletRepository returns a repository.WalletRepository backed by PostgreSQL.
func NewWalletRepository(db DBTX) repository.WalletRepository {
	return &walletRepository{db: db}
}

func (r *walletRepository) CreateWallet(ctx context.Context, wallet *domain.Wallet) error {
	now := time.Now().UTC()
	if wallet.CreatedAt.IsZero() {
		wallet.CreatedAt = now
	}
	wallet.UpdatedAt = now

	query := `
		INSERT INTO wallets (id, name, balance, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := r.db.Exec(ctx, query,
		wallet.ID,
		wallet.Name,
		wallet.Balance,
		wallet.Currency,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("%w: %s", domain.ErrWalletAlreadyExists, wallet.ID)
		}
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	return nil
}

func (r *walletRepository) GetWalletByID(ctx context.Context, id string) (*domain.Wallet, error) {
	query := `
		SELECT id, name, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	var w domain.Wallet
	err := r.db.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&w.Name,
		&w.Balance,
		&w.Currency,
		&w.CreatedAt,
		&w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}
	return &w, nil
}

func (r *walletRepository) GetWalletByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	query := `
		SELECT id, name, balance, currency, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`
	var w domain.Wallet
	err := r.db.QueryRow(ctx, query, id).Scan(
		&w.ID,
		&w.Name,
		&w.Balance,
		&w.Currency,
		&w.CreatedAt,
		&w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("failed to lock wallet for update: %w", err)
	}
	return &w, nil
}

func (r *walletRepository) UpdateWalletBalance(ctx context.Context, id string, newBalance int64) error {
	query := `
		UPDATE wallets
		SET balance = $1, updated_at = $2
		WHERE id = $3
	`
	_, err := r.db.Exec(ctx, query, newBalance, time.Now().UTC(), id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" { // check_violation
			return domain.ErrInsufficientFunds
		}
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}
	return nil
}
