package repository

import (
	"context"
	"wallet-transfer/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WalletRepository interface {
	LockByIDs(
		ctx context.Context,
		tx pgx.Tx,
		ids []string,
	) ([]domain.Wallet, error)

	UpdateBalance(
		ctx context.Context,
		tx pgx.Tx,
		walletID string,
		delta int64,
	) error
}

type walletRepository struct {
	db *pgxpool.Pool
}

func NewWalletRepository(db *pgxpool.Pool) WalletRepository {
	return &walletRepository{db: db}
}

func (r *walletRepository) LockByIDs(
	ctx context.Context,
	tx pgx.Tx,
	ids []string,
) ([]domain.Wallet, error) {

	query := `
	SELECT
		id,
		balance
	FROM wallets
	WHERE id = ANY($1)
	ORDER BY id
	FOR UPDATE
	`

	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var wallets []domain.Wallet

	for rows.Next() {

		var wallet domain.Wallet

		if err := rows.Scan(&wallet.ID, &wallet.Balance); err != nil {
			return nil, err
		}

		wallets = append(wallets, wallet)
	}

	return wallets, rows.Err()
}

func (r *walletRepository) UpdateBalance(
	ctx context.Context,
	tx pgx.Tx,
	walletID string,
	delta int64,
) error {

	query := `
	UPDATE wallets
	SET balance = balance + $1, updated_at = NOW()
	WHERE id = $2
	`

	_, err := tx.Exec(ctx, query, delta, walletID)
	return err
}
