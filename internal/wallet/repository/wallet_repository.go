package repository

import (
	"context"
	"database/sql"
	"fmt"

	"wallet-transfer/internal/wallet/domain"
)

// WalletRepository handles wallet database operations
type WalletRepository struct {
	db *sql.DB
}

// NewWalletRepository creates a new wallet repository
func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

// Create inserts a new wallet
func (r *WalletRepository) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	query := `
		INSERT INTO wallets (wallet_id, user_id, balance_cents, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.ExecContext(ctx, query,
		wallet.ID,
		wallet.UserID,
		wallet.Balance,
		wallet.Currency,
		wallet.CreatedAt,
		wallet.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create wallet: %w", err)
	}
	return nil
}

// GetByID retrieves a wallet by ID
func (r *WalletRepository) GetByID(ctx context.Context, tx *sql.Tx, walletID string) (*domain.Wallet, error) {
	query := `
		SELECT wallet_id, user_id, balance_cents, currency, created_at, updated_at
		FROM wallets
		WHERE wallet_id = $1
	`
	var wallet domain.Wallet
	row := tx.QueryRowContext(ctx, query, walletID)
	err := row.Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.Balance,
		&wallet.Currency,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet: %w", err)
	}
	return &wallet, nil
}

// GetByIDWithLock retrieves a wallet by ID with row lock (FOR UPDATE)
func (r *WalletRepository) GetByIDWithLock(ctx context.Context, tx *sql.Tx, walletID string) (*domain.Wallet, error) {
	query := `
		SELECT wallet_id, user_id, balance_cents, currency, created_at, updated_at
		FROM wallets
		WHERE wallet_id = $1
		FOR UPDATE
	`
	var wallet domain.Wallet
	row := tx.QueryRowContext(ctx, query, walletID)
	err := row.Scan(
		&wallet.ID,
		&wallet.UserID,
		&wallet.Balance,
		&wallet.Currency,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get wallet with lock: %w", err)
	}
	return &wallet, nil
}

// UpdateBalance updates wallet balance
func (r *WalletRepository) UpdateBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error {
	query := `
		UPDATE wallets
		SET balance_cents = $1, updated_at = NOW()
		WHERE wallet_id = $2
	`
	result, err := tx.ExecContext(ctx, query, newBalance, walletID)
	if err != nil {
		return fmt.Errorf("failed to update wallet balance: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("wallet not found")
	}
	return nil
}
