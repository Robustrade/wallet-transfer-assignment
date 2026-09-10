package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

func (r *DBRepository) CreateIdempotencyRecord(
	ctx context.Context,
	db *sql.Tx,
	record *domain.IdempotencyRecord,
) error {
	const query = `
		INSERT INTO idempotency_records (
			idempotency_key,
			request_hash,
			created_at,
			expires_at
		)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING
	`

	result, err := db.ExecContext(
		ctx,
		query,
		record.IdempotencyKey,
		record.RequestHash,
		record.CreatedAt,
		record.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create idempotency record: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get idempotency rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return domain.ErrIdempotencyKeyExists
	}

	return nil
}

func (r *DBRepository) GetIdempotencyRecord(
	ctx context.Context,
	db *sql.Tx,
	key string,
) (*domain.IdempotencyRecord, error) {
	const query = `
		SELECT
			idempotency_key,
			request_hash,
			transfer_id,
			response_code,
			response_body,
			created_at,
			expires_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`

	var record domain.IdempotencyRecord

	err := db.QueryRowContext(ctx, query, key).Scan(
		&record.IdempotencyKey,
		&record.RequestHash,
		&record.TransferID,
		&record.ResponseCode,
		&record.ResponseBody,
		&record.CreatedAt,
		&record.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrIdempotencyNotFound
		}

		return nil, fmt.Errorf("get idempotency record: %w", err)
	}

	return &record, nil
}

func (r *DBRepository) UpdateResult(
	ctx context.Context,
	db *sql.Tx,
	record *domain.IdempotencyRecord,
) error {
	const query = `
		UPDATE idempotency_records
		SET transfer_id = $1,
		    response_code = $2,
		    response_body = $3
		WHERE idempotency_key = $4
	`

	result, err := db.ExecContext(
		ctx,
		query,
		record.TransferID,
		record.ResponseCode,
		record.ResponseBody,
		record.IdempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("update idempotency result: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get idempotency rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return domain.ErrIdempotencyNotFound
	}

	return nil
}
