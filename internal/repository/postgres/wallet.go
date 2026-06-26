package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

// WalletRepository is the PostgreSQL adapter for wallet persistence.
type WalletRepository struct {
	db *DB
}

// NewWalletRepository constructs a WalletRepository over db.
func NewWalletRepository(db *DB) *WalletRepository {
	return &WalletRepository{db: db}
}

var _ service.WalletRepository = (*WalletRepository)(nil)

// Create inserts a new wallet row.
func (r *WalletRepository) Create(ctx context.Context, wallet *domain.Wallet) error {
	_, err := r.db.conn(ctx).Exec(ctx,
		`INSERT INTO wallets (id, balance, created_at, updated_at)
		 VALUES ($1, $2, now(), now())`,
		wallet.ID, wallet.Balance,
	)
	if err != nil {
		return fmt.Errorf("postgres: create wallet: %w", err)
	}
	return nil
}

// Get returns a wallet without locking, or domain.ErrWalletNotFound.
func (r *WalletRepository) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, id, false)
}

// GetForUpdate returns a wallet while acquiring a row-level write lock
// (SELECT ... FOR UPDATE), serialising concurrent debits/credits on it.
func (r *WalletRepository) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.get(ctx, id, true)
}

func (r *WalletRepository) get(ctx context.Context, id string, forUpdate bool) (*domain.Wallet, error) {
	query := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}

	var w domain.Wallet
	err := r.db.conn(ctx).QueryRow(ctx, query, id).
		Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("postgres: get wallet: %w", err)
	}
	return &w, nil
}

// UpdateBalance persists a new balance for an existing wallet.
func (r *WalletRepository) UpdateBalance(ctx context.Context, id string, balance domain.Money) error {
	tag, err := r.db.conn(ctx).Exec(ctx,
		`UPDATE wallets SET balance = $2, updated_at = now() WHERE id = $1`,
		id, balance,
	)
	if err != nil {
		return fmt.Errorf("postgres: update wallet balance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}
