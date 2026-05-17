package repository

import (
	"context"
	"database/sql"
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

func (r *WalletRepository) GetWalletByIDForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {

	query := `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`

	row := tx.QueryRowContext(ctx, query, id)

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

func (r *WalletRepository) UpdateWalletBalance(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	balance int64,
) error {

	query := `
		UPDATE wallets
		SET balance = $1,
		    updated_at = NOW()
		WHERE id = $2
	`

	_, err := tx.ExecContext(ctx, query, balance, id)
	return err
}
