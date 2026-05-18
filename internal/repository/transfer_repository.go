package repository

import (
	"context"

	"wallet-transfer-service/internal/domain"

	"github.com/jackc/pgx/v5"
)

type TransferRepository struct {
}

func NewTransferRepository() *TransferRepository {
	return &TransferRepository{}
}

func (r *TransferRepository) CreateTransfer(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) error {

	query := `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, NOW(), NOW()
		)
	`

	_, err := tx.Exec(
		ctx,
		query,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)

	return err
}

func (r *TransferRepository) GetTransferByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	key string,
) (*domain.Transfer, error) {

	query := `
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`

	row := tx.QueryRow(ctx, query, key)

	var transfer domain.Transfer

	err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &transfer, nil
}
