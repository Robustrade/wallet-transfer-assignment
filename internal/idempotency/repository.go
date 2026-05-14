package idempotency

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

// IdempotencyRepository handles idempotency key storage
type IdempotencyRepository struct {
	db *sql.DB
}

// NewIdempotencyRepository creates a new idempotency repository
func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// ErrDuplicateIdempotencyKey indicates the idempotency key was already recorded
var ErrDuplicateIdempotencyKey = fmt.Errorf("duplicate idempotency key")

// RecordKey stores an idempotency key with the response
func (r *IdempotencyRepository) RecordKey(ctx context.Context, tx *sql.Tx, idempotencyKey string, transferID string, responseBody interface{}) error {
	responseJSON, err := json.Marshal(responseBody)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	query := `
		INSERT INTO idempotency_records (idempotency_key, transfer_id, response_body, created_at)
		VALUES ($1, $2, $3, NOW())
	`
	_, err = tx.ExecContext(ctx, query, idempotencyKey, transferID, responseJSON)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("failed to record idempotency key: %w", err)
	}
	return nil
}

// GetKey retrieves an idempotency key record
func (r *IdempotencyRepository) GetKey(ctx context.Context, idempotencyKey string) (transferID string, responseBody []byte, exists bool, err error) {
	query := `
		SELECT transfer_id, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1
	`
	row := r.db.QueryRowContext(ctx, query, idempotencyKey)
	err = row.Scan(&transferID, &responseBody)
	if err == sql.ErrNoRows {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("failed to get idempotency record: %w", err)
	}
	return transferID, responseBody, true, nil
}
