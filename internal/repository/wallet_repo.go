package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"wallet-transfer-service/internal/apperror"
	"wallet-transfer-service/internal/model"

	"github.com/jackc/pgx/v5/pgconn"
)

type WalletRepository interface {
	Create(ctx context.Context, wallet *model.Wallet) error
	GetByID(ctx context.Context, id string) (*model.Wallet, error)
	UpdateStatus(ctx context.Context, id string, status model.WalletStatus) error
}

type walletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) WalletRepository {
	return &walletRepository{db: db}
}

// Create inserts a new wallet record into the database.
func (r *walletRepository) Create(ctx context.Context, wallet *model.Wallet) error {
	slog.DebugContext(ctx, "wallet repository create started", "wallet_id", wallet.ID)
	if wallet.Status == "" {
		wallet.Status = model.WalletStatusActive
	}

	query := `
		INSERT INTO wallets (id, balance, status, created_at, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING created_at, updated_at
	`
	err := r.db.QueryRowContext(
		ctx,
		query,
		wallet.ID,
		wallet.Balance,
		wallet.Status,
	).Scan(&wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			slog.WarnContext(ctx, "wallet repository create conflict", "wallet_id", wallet.ID)
			return apperror.ErrWalletAlreadyExists
		}
		slog.ErrorContext(ctx, "wallet repository create failed", "wallet_id", wallet.ID, "error", err)
		return fmt.Errorf("failed to create wallet: %w", err)
	}

	slog.DebugContext(ctx, "wallet repository create completed", "wallet_id", wallet.ID)
	return nil
}

// GetByID retrieves a single wallet and its current balance and status by ID.
func (r *walletRepository) GetByID(ctx context.Context, id string) (*model.Wallet, error) {
	slog.DebugContext(ctx, "wallet repository lookup started", "wallet_id", id)
	query := `
		SELECT id, balance, status, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)

	var w model.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.DebugContext(ctx, "wallet repository lookup returned no rows", "wallet_id", id)
			return nil, apperror.ErrWalletNotFound
		}
		slog.ErrorContext(ctx, "wallet repository lookup failed", "wallet_id", id, "error", err)
		return nil, fmt.Errorf("failed to query wallet: %w", err)
	}

	slog.DebugContext(ctx, "wallet repository lookup completed", "wallet_id", id)
	return &w, nil
}

// UpdateStatus modifies the status enum (ACTIVE, FROZEN, DEBIT_FROZEN, CREDIT_FROZEN) of a wallet.
func (r *walletRepository) UpdateStatus(ctx context.Context, id string, status model.WalletStatus) error {
	slog.DebugContext(ctx, "wallet repository status update started", "wallet_id", id, "status", status)
	query := `
		UPDATE wallets
		SET status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`
	res, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		slog.ErrorContext(ctx, "wallet repository status update failed", "wallet_id", id, "error", err)
		return fmt.Errorf("failed to update wallet status: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		slog.ErrorContext(ctx, "wallet repository affected rows lookup failed", "wallet_id", id, "error", err)
		return fmt.Errorf("failed reading affected rows: %w", err)
	}
	if rowsAffected == 0 {
		slog.DebugContext(ctx, "wallet repository status update returned no rows", "wallet_id", id)
		return apperror.ErrWalletNotFound
	}

	slog.DebugContext(ctx, "wallet repository status update completed", "wallet_id", id, "status", status)
	return nil
}
