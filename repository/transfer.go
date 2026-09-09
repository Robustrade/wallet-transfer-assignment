package repository

import (
	"context"
	"database/sql"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

type TransferRepository struct {
	db *sql.DB
}

func NewTransferRepository(db *sql.DB) *TransferRepository {
	return &TransferRepository{db: db}
}

func (r *TransferRepository) Create(
	ctx context.Context,
	tx *sql.Tx,
	transfer *model.Transfer,
) (bool, error) {

	result, err := tx.ExecContext(ctx, `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (idempotency_key) DO NOTHING
	`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.Status,
	)

	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rows == 1, nil
}

func (r *TransferRepository) GetByIdempotencyKey(
	ctx context.Context,
	tx *sql.Tx,
	key string,
) (*model.Transfer, error) {

	var transfer model.Transfer

	err := tx.QueryRowContext(ctx, `
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
	`,
		key,
	).Scan(
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

func (r *TransferRepository) UpdateStatus(
	ctx context.Context,
	tx *sql.Tx,
	id string,
	status model.TransferStatus,
) error {

	_, err := tx.ExecContext(ctx, `
		UPDATE transfers
		SET status = $1,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, status, id)

	return err
}
