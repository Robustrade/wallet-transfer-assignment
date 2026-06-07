package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/jackc/pgx/v5"
)

// walletRepo implements repository.WalletRepository against PostgreSQL.
type walletRepo struct {
	q querier
}

const walletColumns = "id, balance, currency, created_at, updated_at"

// Create inserts a wallet, translating a unique violation into a domain error.
func (r *walletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	const query = `
		INSERT INTO wallets (id, balance, currency, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`
	_, err := r.q.Exec(ctx, query, w.ID, w.Balance, w.Currency, w.CreatedAt, w.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrWalletAlreadyExists
		}
		return fmt.Errorf("insert wallet: %w", err)
	}
	return nil
}

// Get reads a wallet without locking.
func (r *walletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	const query = "SELECT " + walletColumns + " FROM wallets WHERE id = $1"
	return scanWallet(r.q.QueryRow(ctx, query, id))
}

// GetForUpdate reads a wallet and locks its row until the surrounding
// transaction ends. This is the primitive that serialises concurrent debits on
// the same wallet and prevents the read-then-write race.
func (r *walletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	const query = "SELECT " + walletColumns + " FROM wallets WHERE id = $1 FOR UPDATE"
	return scanWallet(r.q.QueryRow(ctx, query, id))
}

// UpdateBalance persists a new balance and bumps updated_at.
func (r *walletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	const query = `
		UPDATE wallets
		SET balance = $2, updated_at = now()
		WHERE id = $1`
	tag, err := r.q.Exec(ctx, query, id, newBalance)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

// scanWallet maps a single row into a domain.Wallet.
func scanWallet(row pgx.Row) (*domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}
	return &w, nil
}
