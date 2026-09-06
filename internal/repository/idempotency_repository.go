package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

var ErrIdempotencyKeyExists = errors.New("idempotency key already exists")

type idempotencyRepository struct{}

func NewIdempotencyRepository() IdempotencyRepository {
	return &idempotencyRepository{}
}

func (r *idempotencyRepository) Create(ctx context.Context, tx *sql.Tx, key, id string, transferId string) error {
	const query = `
		INSERT INTO idempotency_records (
			id,
			idempotency_key,
			transfer_id,
			created_at
		)
		VALUES ($1, $2, $3, NOW())
	`

	_, err := tx.ExecContext(ctx, query, key, id, transferId)
	if err == nil {
		return nil
	}

	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrIdempotencyKeyExists
	}

	return err
}

func (r *idempotencyRepository) GetByKey(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var transferId string

	const query = `
		SELECT transfer_id
		FROM idempotency_records
		WHERE idempotency_key = $1
	`

	err := tx.QueryRowContext(ctx, query, key).Scan(&transferId)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}

	if err != nil {
		return "", err
	}

	return transferId, nil
}
