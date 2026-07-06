package repository

import (
	"context"
	"database/sql"
	"log"

	"wallet-transfer/internal/domain"
)

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

// GetBalance fetches the current balance for a given wallet ID
func (r *WalletRepository) GetBalance(ctx context.Context, walletID string) (float64, error) {
	var balance float64

	err := r.db.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id = $1", walletID).Scan(&balance)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, domain.ErrWalletNotFound
		}
		log.Printf("[Repository] GetBalance - DB Error for wallet %s: %v", walletID, err)
		return 0, err
	}

	return balance, nil
}
