package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

type IdempotencyRepository struct {
	db *db
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{db: &db{pool: pool}}
}

// Claim tries to atomically insert a new PENDING record for key. The
// INSERT ... ON CONFLICT DO NOTHING is what makes this race-free: whichever
// concurrent request's insert actually lands is the sole owner of the key.
func (r *IdempotencyRepository) Claim(ctx context.Context, key, requestHash string) (*domain.IdempotencyRecord, bool, error) {
	row := r.db.q(ctx).QueryRow(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		VALUES ($1, $2, 'PENDING')
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key, request_hash, status, transfer_id, created_at, updated_at
	`, key, requestHash)

	rec, err := scanIdempotencyRecord(row)
	if err == nil {
		return rec, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("claim idempotency key: %w", err)
	}

	existing, getErr := r.Get(ctx, key)
	if getErr != nil {
		return nil, false, getErr
	}
	return existing, false, nil
}

func (r *IdempotencyRepository) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	row := r.db.q(ctx).QueryRow(ctx, `
		SELECT idempotency_key, request_hash, status, transfer_id, created_at, updated_at
		FROM idempotency_records WHERE idempotency_key = $1
	`, key)

	rec, err := scanIdempotencyRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrIdempotencyRecordNotFound
		}
		return nil, err
	}
	return rec, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, key, transferID string) error {
	tag, err := r.db.q(ctx).Exec(ctx, `
		UPDATE idempotency_records
		SET status = 'COMPLETED', transfer_id = $1, updated_at = now()
		WHERE idempotency_key = $2
	`, transferID, key)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrIdempotencyRecordNotFound
	}
	return nil
}

// Release deletes a PENDING claim that never resulted in a durable transfer,
// so the key is free to be claimed again by a future retry.
func (r *IdempotencyRepository) Release(ctx context.Context, key string) error {
	_, err := r.db.q(ctx).Exec(ctx, `
		DELETE FROM idempotency_records WHERE idempotency_key = $1 AND status = 'PENDING'
	`, key)
	if err != nil {
		return fmt.Errorf("release idempotency claim: %w", err)
	}
	return nil
}

func scanIdempotencyRecord(row pgx.Row) (*domain.IdempotencyRecord, error) {
	var rec domain.IdempotencyRecord
	var transferID *string

	if err := row.Scan(&rec.IdempotencyKey, &rec.RequestHash, &rec.Status, &transferID,
		&rec.CreatedAt, &rec.UpdatedAt); err != nil {
		return nil, err
	}

	if transferID != nil {
		rec.TransferID = *transferID
	}
	return &rec, nil
}
