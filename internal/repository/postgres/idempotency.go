package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type idempotencyRepo struct{ q querier }

func (r idempotencyRepo) Claim(ctx context.Context, rec service.IdempotencyRecord) (bool, error) {
	// On a key held by another in-flight transaction, Postgres blocks this INSERT on
	// the primary-key index until that transaction ends, then either inserts (it rolled
	// back) or does nothing (it committed). That is what serialises duplicate requests.
	tag, err := r.q.Exec(ctx, `
		INSERT INTO idempotency_records (key, request_hash, transfer_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO NOTHING`,
		rec.Key, rec.RequestHash, rec.TransferID)
	if err != nil {
		return false, translate(fmt.Errorf("claim idempotency key: %w", err))
	}
	return tag.RowsAffected() == 1, nil
}

func (r idempotencyRepo) Get(ctx context.Context, key string) (service.IdempotencyRecord, error) {
	var rec service.IdempotencyRecord
	err := r.q.QueryRow(ctx, `
		SELECT key, request_hash, transfer_id, created_at FROM idempotency_records WHERE key = $1`, key,
	).Scan(&rec.Key, &rec.RequestHash, &rec.TransferID, &rec.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return service.IdempotencyRecord{}, fmt.Errorf("idempotency record not found after conflict: %w", err)
	}
	if err != nil {
		return service.IdempotencyRecord{}, translate(fmt.Errorf("load idempotency record: %w", err))
	}
	return rec, nil
}
