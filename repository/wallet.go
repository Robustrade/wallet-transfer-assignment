package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

func (r *WalletRepository) GetForUpdate(
	ctx context.Context,
	tx *sql.Tx,
	walletID string,
) (*model.Wallet, error) {

	var wallet model.Wallet

	err := tx.QueryRowContext(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, walletID).Scan(
		&wallet.ID,
		&wallet.Balance,
		&wallet.CreatedAt,
		&wallet.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("wallet not found: %s", walletID)
		}
		return nil, err
	}

	return &wallet, nil
}

func (r *WalletRepository) UpdateBalance(
	ctx context.Context,
	tx *sql.Tx,
	walletID string,
	balance int64,
) error {

	_, err := tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = $1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, balance, walletID)

	return err
}
