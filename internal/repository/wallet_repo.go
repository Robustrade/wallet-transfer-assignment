package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// WalletRepository handles wallet persistence operations.
type WalletRepository struct{}

// NewWalletRepository creates a new WalletRepository.
func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

// Create inserts a new wallet into the database.
func (r *WalletRepository) Create(ctx context.Context, db DBTX, wallet *domain.Wallet) (*domain.Wallet, error) {
	err := db.QueryRow(ctx,
		`INSERT INTO wallets (id, balance, currency)
		 VALUES ($1, $2, $3)
		 RETURNING created_at, updated_at`,
		wallet.ID, wallet.Balance, wallet.Currency,
	).Scan(&wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, domain.ErrDuplicateWallet
		}
		return nil, err
	}
	return wallet, nil
}

// GetByID retrieves a wallet by its ID.
func (r *WalletRepository) GetByID(ctx context.Context, db DBTX, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := db.QueryRow(ctx,
		`SELECT id, balance, currency, created_at, updated_at
		 FROM wallets WHERE id = $1`,
		id,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.Currency, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

// GetByIDForUpdate retrieves a wallet with a row-level lock (SELECT FOR UPDATE).
// Must be called within a transaction. Used to prevent concurrent modifications
// to the same wallet during transfer processing.
func (r *WalletRepository) GetByIDForUpdate(ctx context.Context, db DBTX, id string) (*domain.Wallet, error) {
	wallet := &domain.Wallet{}
	err := db.QueryRow(ctx,
		`SELECT id, balance, currency, created_at, updated_at
		 FROM wallets WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(&wallet.ID, &wallet.Balance, &wallet.Currency, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return wallet, nil
}

// UpdateBalance adjusts a wallet's balance by the given delta.
// Positive delta = credit, negative delta = debit.
// The database CHECK constraint (balance >= 0) prevents negative balances.
func (r *WalletRepository) UpdateBalance(ctx context.Context, db DBTX, id string, delta int64) error {
	result, err := db.Exec(ctx,
		`UPDATE wallets SET balance = balance + $1, updated_at = NOW()
		 WHERE id = $2`,
		delta, id,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}
