package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type IdempotencyRepo struct {
	db *sql.DB
}

func NewIdempotencyRepo(db *sql.DB) *IdempotencyRepo {
	return &IdempotencyRepo{db: db}
}

func (r *IdempotencyRepo) RecordKey(ctx context.Context, tx *sql.Tx, key, transferID string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO idempotency_records (key, transfer_id) VALUES ($1, $2)`,
		key, transferID,
	)
	if err != nil {
		return fmt.Errorf("record idempotency key: %w", err)
	}
	return nil
}

func (r *IdempotencyRepo) GetTransferID(ctx context.Context, key string) (string, error) {
	var transferID string
	err := r.db.QueryRowContext(ctx,
		`SELECT transfer_id FROM idempotency_records WHERE key = $1`,
		key,
	).Scan(&transferID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get idempotency record: %w", err)
	}
	return transferID, nil
}
