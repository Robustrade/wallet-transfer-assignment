package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/skc1234/wallet-transfer-assignment/internal/domain"
)

type WalletRepository struct {
	DB *pgxpool.Pool
}

func NewWalletRepository(db *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{
		DB: db,
	}
}

// Create a new wallet
func (r *WalletRepository) CreateWallet(
	ctx context.Context,
	balance float64,
) (string, error) {

	var walletID string

	query := `
		INSERT INTO wallets (id, balance)
		VALUES (gen_random_uuid(), $1)
		RETURNING id
	`

	err := r.DB.QueryRow(
		ctx,
		query,
		balance,
	).Scan(&walletID)

	if err != nil {
		return "", err
	}

	return walletID, nil
}

// Get wallet by ID
func (r *WalletRepository) GetWallet(
	ctx context.Context,
	id string,
) (domain.Wallet, error) {

	var wallet domain.Wallet

	query := `
		SELECT id, balance, created_at
		FROM wallets
		WHERE id = $1
	`

	err := r.DB.QueryRow(
		ctx,
		query,
		id,
	).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
	)

	if err != nil {
		return domain.Wallet{}, err
	}

	wallet.Currency = "INR"

	return wallet, nil
}
