package repository

import (
	"context"
	"wallet-transfer-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type WalletRepository struct {
	db *pgx.Conn
}

func NewWalletRepository(db *pgx.Conn) *WalletRepository {
	return &WalletRepository{
		db: db,
	}
}

func (r *WalletRepository) GetWalletByID(ctx context.Context, id string) (*domain.Wallet, error) {

	query := `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`

	row := r.db.QueryRow(ctx, query, id)

	var wallet domain.Wallet

	err := row.Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &wallet, nil
}
