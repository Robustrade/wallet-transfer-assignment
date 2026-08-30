package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
)

type PostgresTransferRepository struct{}

func NewPostgresTransferRepository() *PostgresTransferRepository {
	return &PostgresTransferRepository{}
}

func (r *PostgresTransferRepository) GetByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	key string,
) (*domain.Transfer, error) {
	const query = `
		SELECT
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			request_hash,
			created_at,
			updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`

	var transfer domain.Transfer

	err := tx.QueryRow(ctx, query, key).Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.RequestHash,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("get transfer by idempotency key: %w", err)
	}

	return &transfer, nil
}

func (r *PostgresTransferRepository) Create(
	ctx context.Context,
	tx pgx.Tx,
	transfer *domain.Transfer,
) error {
	const query = `
		INSERT INTO transfers (
			id,
			idempotency_key,
			from_wallet_id,
			to_wallet_id,
			amount,
			status,
			request_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
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
		transfer.RequestHash,
	)

	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}

	return nil
}

func (r *PostgresTransferRepository) UpdateStatus(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	status domain.TransferStatus,
) error {
	const query = `
		UPDATE transfers
		SET status = $1,
		    updated_at = NOW()
		WHERE id = $2
	`

	result, err := tx.Exec(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	if result.RowsAffected() != 1 {
		return fmt.Errorf("transfer not found: %s", id)
	}

	return nil
}
