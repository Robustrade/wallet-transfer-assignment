package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdempotencyRepository struct {
	pool *pgxpool.Pool
}

func NewIdempotencyRepository(pool *pgxpool.Pool) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool}
}

func (r *IdempotencyRepository) GetIdempotencyRecord(ctx context.Context, tx pgx.Tx, key string) (*domain.IdempotencyRecord, error) {
	query := `
		SELECT idempotency_key, request_hash, transfer_id, response_code, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1
	`
	var rec domain.IdempotencyRecord
	var err error

	if tx != nil {
		err = tx.QueryRow(ctx, query, key).Scan(
			&rec.Key,
			&rec.RequestHash,
			&rec.TransferID,
			&rec.ResponseCode,
			&rec.ResponseBody,
		)
	} else {
		err = r.pool.QueryRow(ctx, query, key).Scan(
			&rec.Key,
			&rec.RequestHash,
			&rec.TransferID,
			&rec.ResponseCode,
			&rec.ResponseBody,
		)
	}

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // Record does not exist
		}
		return nil, domain.ErrInternal(fmt.Errorf("failed fetching idempotency record: %w", err))
	}

	return &rec, nil
}

func (r *IdempotencyRepository) SaveIdempotencyRecord(ctx context.Context, tx pgx.Tx, rec *domain.IdempotencyRecord) error {
	query := `
		INSERT INTO idempotency_records (idempotency_key, request_hash, transfer_id, response_code, response_body, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	now := time.Now().UTC()
	var err error
	if tx != nil {
		_, err = tx.Exec(ctx, query, rec.Key, rec.RequestHash, rec.TransferID, rec.ResponseCode, rec.ResponseBody, now, now)
	} else {
		_, err = r.pool.Exec(ctx, query, rec.Key, rec.RequestHash, rec.TransferID, rec.ResponseCode, rec.ResponseBody, now, now)
	}

	if err != nil {
		return domain.ErrInternal(fmt.Errorf("failed saving idempotency record: %w", err))
	}
	return nil
}
