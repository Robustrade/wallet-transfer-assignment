package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
)

type walletRepo struct{}

func NewWalletRepository() WalletRepository {
	return &walletRepo{}
}

func (r *walletRepo) GetByID(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*domain.Wallet, error) {

	var wallet domain.Wallet

	err := tx.QueryRow(
		ctx,
		`
		SELECT
			id,
			balance
		FROM wallets
		WHERE id=$1
		`,
		id,
	).Scan(
		&wallet.ID,
		&wallet.Balance,
	)

	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

func (r *walletRepo) GetForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
) (*domain.Wallet, error) {

	var wallet domain.Wallet

	err := tx.QueryRow(
		ctx,
		`
		SELECT
			id,
			balance
		FROM wallets
		WHERE id=$1
		FOR UPDATE
		`,
		id,
	).Scan(
		&wallet.ID,
		&wallet.Balance,
	)

	if err != nil {
		return nil, err
	}

	return &wallet, nil
}

func (r *walletRepo) UpdateBalance(
	ctx context.Context,
	tx pgx.Tx,
	id uuid.UUID,
	balance int64,
) error {

	_, err := tx.Exec(
		ctx,
		`
		UPDATE wallets
		SET balance=$1
		WHERE id=$2
		`,
		balance,
		id,
	)

	return err
}
