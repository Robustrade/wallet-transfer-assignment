package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// IdempotencyRepository handles the idempotency_records table: storing and
// replaying full request/response outcomes for a given idempotency key.
type IdempotencyRepository interface {
	// Get returns the stored record for a key, or nil if none exists.
	Get(ctx context.Context, tx pgx.Tx, key string) (*IdempotencyRecord, error)
	// Save persists the outcome of processing a request under a given key.
	Save(ctx context.Context, tx pgx.Tx, rec *IdempotencyRecord) error
}

// IdempotencyRecord is the stored outcome of a previously handled request.
type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	TransferID     uuid.UUID
	ResponseBody   []byte
	StatusCode     int
}

type pgxIdempotencyRepository struct{}

func NewIdempotencyRepository() IdempotencyRepository {
	return &pgxIdempotencyRepository{}
}

// Get returns the stored record for a key, or nil (no error) if not found.
func (r *pgxIdempotencyRepository) Get(ctx context.Context, tx pgx.Tx, key string) (*IdempotencyRecord, error) {
	const query = `
		SELECT idempotency_key, request_hash, COALESCE(transfer_id::text, ''), response_body, status_code
		FROM idempotency_records
		WHERE idempotency_key = $1`

	var rec IdempotencyRecord
	var transferIDStr string
	err := tx.QueryRow(ctx, query, key).Scan(
		&rec.IdempotencyKey, &rec.RequestHash, &transferIDStr, &rec.ResponseBody, &rec.StatusCode,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("looking up idempotency record: %w", err)
	}
	return &rec, nil
}

// Save upserts the outcome of processing a request under a given key.
// ON CONFLICT DO NOTHING because once a key's result is recorded, it should
// never be overwritten by a racing duplicate — first writer wins.
func (r *pgxIdempotencyRepository) Save(ctx context.Context, tx pgx.Tx, rec *IdempotencyRecord) error {
	const query = `
		INSERT INTO idempotency_records (idempotency_key, request_hash, transfer_id, response_body, status_code)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (idempotency_key) DO NOTHING`

	_, err := tx.Exec(ctx, query, rec.IdempotencyKey, rec.RequestHash, rec.TransferID, rec.ResponseBody, rec.StatusCode)
	if err != nil {
		return fmt.Errorf("saving idempotency record: %w", err)
	}
	return nil
}
