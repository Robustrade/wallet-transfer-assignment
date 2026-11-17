package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IdempotencyRepository is the DB access for idempotency rows. Replay/mismatch decisions live in service.
type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

// Claim inserts the key. claimed=true means this transaction owns the key.
// claimed=false means the key already exists (caller should load and replay/mismatch).
func (r *IdempotencyRepository) Claim(ctx context.Context, rec *model.IdempotencyRecord) (claimed bool, err error) {
	q := QuerierFrom(ctx, r.pool)
	var key string
	err = q.QueryRow(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, request_body, created_at, updated_at)
		VALUES (@idempotency_key, @request_hash, @request_body::jsonb, @created_at, @updated_at)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key`,
		pgx.NamedArgs{
			"idempotency_key": rec.IdempotencyKey,
			"request_hash":    rec.RequestHash,
			"request_body":    []byte(rec.RequestBody),
			"created_at":      rec.CreatedAt,
			"updated_at":      rec.UpdatedAt,
		},
	).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return true, nil
}

// Get loads an idempotency record or model.ErrNotFound.
func (r *IdempotencyRepository) Get(ctx context.Context, key string) (*model.IdempotencyRecord, error) {
	q := QuerierFrom(ctx, r.pool)
	var rec model.IdempotencyRecord
	var body []byte
	var respBody []byte
	err := q.QueryRow(ctx, `
		SELECT idempotency_key, request_hash, request_body,
		       response_code, response_body, created_at, updated_at
		FROM idempotency_records WHERE idempotency_key = @idempotency_key`,
		pgx.NamedArgs{"idempotency_key": key},
	).Scan(
		&rec.IdempotencyKey, &rec.RequestHash, &body,
		&rec.ResponseCode, &respBody, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency: %w", err)
	}
	rec.RequestBody = body
	rec.ResponseBody = respBody
	return &rec, nil
}

// StoreResponse writes the terminal HTTP outcome once.
func (r *IdempotencyRepository) StoreResponse(ctx context.Context, key string, code int, body []byte) error {
	q := QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE idempotency_records
		SET response_code = @response_code, response_body = @response_body::jsonb, updated_at = @updated_at
		WHERE idempotency_key = @idempotency_key AND response_code IS NULL`,
		pgx.NamedArgs{
			"idempotency_key": key,
			"response_code":   code,
			"response_body":   body,
			"updated_at":      time.Now().UTC(),
		},
	)
	if err != nil {
		return fmt.Errorf("store idempotency response: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return model.ErrConflict
	}
	return nil
}
