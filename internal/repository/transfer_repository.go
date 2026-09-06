package repository

import (
	"context"
	"database/sql"
	"errors"

	"wallet-transfer-assignment/internal/domain"
)

var ErrTransferNotFound = errors.New("transfer not found")

type transferRepository struct{}

func NewTransferRepository() TransferRepository {
	return &transferRepository{}
}

func (r *transferRepository) Create(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer) error {
	const query = `
		INSERT INTO transfers (
			id, idempotency_key, from_wallet_id, to_wallet_id,
			amount, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := tx.ExecContext(ctx, query, transfer.Id, transfer.IdempotencyKey, transfer.FromWalletId,
		transfer.ToWalletId, transfer.Amount, transfer.Status, transfer.CreatedAt, transfer.UpdatedAt)

	return err
}

func (r *transferRepository) GetById(ctx context.Context, tx *sql.Tx, transferId string) (*domain.Transfer, error) {
	const query = `
		SELECT
			id, idempotency_key, from_wallet_id, to_wallet_id,
			amount, status, created_at, updated_at
		FROM transfers
		WHERE id = $1
	`

	var transfer domain.Transfer

	err := tx.QueryRowContext(ctx, query, transferId).Scan(&transfer.Id, &transfer.IdempotencyKey,
		&transfer.FromWalletId, &transfer.ToWalletId, &transfer.Amount, &transfer.Status,
		&transfer.CreatedAt, &transfer.UpdatedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTransferNotFound
	}

	if err != nil {
		return nil, err
	}

	return &transfer, nil
}

func (r *transferRepository) UpdateStatus(ctx context.Context, tx *sql.Tx, transferId string, transferStatus domain.TransferStatus) error {
	const query = `
		UPDATE transfers
			SET status = $1,
				updated_at = NOW()
			WHERE id = $2
	`

	result, err := tx.ExecContext(ctx, query, transferStatus, transferId)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return ErrTransferNotFound
	}

	return nil
}
