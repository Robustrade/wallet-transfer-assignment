package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
)

type idempotencyRepo struct{ db dbtx }

// Claim relies on the primary key on idempotency_records.key. If a concurrent
// transaction has inserted the same key but not yet committed, the INSERT
// blocks on that row until the other transaction finishes: afterwards it either
// conflicts (the first request won) or inserts (the first one rolled back).
func (r *idempotencyRepo) Claim(ctx context.Context, key, requestHash string, now time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO idempotency_records (key, request_hash, created_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (key) DO NOTHING`, key, requestHash, now)
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *idempotencyRepo) Get(ctx context.Context, key string) (*repository.IdempotencyRecord, error) {
	var rec repository.IdempotencyRecord
	err := r.db.QueryRow(ctx,
		`SELECT key, request_hash, transfer_id, created_at FROM idempotency_records WHERE key = $1`, key).
		Scan(&rec.Key, &rec.RequestHash, &rec.TransferID, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}
	rec.CreatedAt = rec.CreatedAt.UTC()
	return &rec, nil
}

func (r *idempotencyRepo) AttachTransfer(ctx context.Context, key string, transferID uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE idempotency_records SET transfer_id = $2 WHERE key = $1`, key, transferID)
	if err != nil {
		return fmt.Errorf("attach transfer to idempotency record: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("attach transfer: idempotency key %q not claimed", key)
	}
	return nil
}
