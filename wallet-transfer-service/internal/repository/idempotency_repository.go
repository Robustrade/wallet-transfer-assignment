package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  uuid.UUID
}

type IdempotencyRepository struct{}

func NewIdempotencyRepository() *IdempotencyRepository { return &IdempotencyRepository{} }

func (r *IdempotencyRepository) Get(ctx context.Context, db DBTX, key string) (IdempotencyRecord, error) {
	const q = `
        SELECT key, request_hash, transfer_id
        FROM idempotency_records
        WHERE key = $1`

	var rec IdempotencyRecord
	err := db.QueryRow(ctx, q, key).Scan(&rec.Key, &rec.RequestHash, &rec.TransferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdempotencyRecord{}, pgx.ErrNoRows
	}
	if err != nil {
		return IdempotencyRecord{}, fmt.Errorf("get idempotency record: %w", err)
	}
	return rec, nil
}

func (r *IdempotencyRepository) Create(ctx context.Context, db DBTX, rec IdempotencyRecord) error {
	const q = `
        INSERT INTO idempotency_records (key, request_hash, transfer_id, created_at)
        VALUES ($1, $2, $3, NOW())`

	_, err := db.Exec(ctx, q, rec.Key, rec.RequestHash, rec.TransferID)
	if err != nil {
		return fmt.Errorf("create idempotency record: %w", err)
	}
	return nil
}

// CompareRequest returns a domain-level conflict when the same key is used
// for a different request payload.
func CompareRequest(existing IdempotencyRecord, requestHash string) error {
	if existing.RequestHash != requestHash {
		return domain.ErrIdempotencyConflict
	}
	return nil
}
