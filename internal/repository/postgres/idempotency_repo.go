package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"Wallet_Transfer_Service/internal/domain"
)

type idempotencyRepo struct {
	db dbtx
}

func (r *idempotencyRepo) Claim(ctx context.Context, key, requestHash string) (bool, *domain.IdempotencyRecord, error) {
	const insertQ = `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status, created_at, updated_at)
		VALUES ($1, $2, $3, now(), now())
		ON CONFLICT (idempotency_key) DO NOTHING`
	res, err := r.db.ExecContext(ctx, insertQ, key, requestHash, domain.IdempotencyInProgress)
	if err != nil {
		return false, nil, fmt.Errorf("claim idempotency key: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("claim idempotency key: %w", err)
	}
	if n == 1 {
		return true, nil, nil
	}

	existing, err := r.Get(ctx, key)
	if err != nil {
		return false, nil, err
	}
	return false, existing, nil
}

func (r *idempotencyRepo) Complete(ctx context.Context, key, transferID string, responseStatus int, responseBody []byte) error {
	const q = `
		UPDATE idempotency_records
		SET status = $1, transfer_id = $2, response_status = $3, response_body = $4, updated_at = now()
		WHERE idempotency_key = $5`
	res, err := r.db.ExecContext(ctx, q, domain.IdempotencyCompleted, transferID, responseStatus, responseBody, key)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("complete idempotency record: key %q not claimed", key)
	}
	return nil
}

func (r *idempotencyRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	const q = `
		SELECT idempotency_key, request_hash, status, COALESCE(transfer_id, ''),
		       COALESCE(response_status, 0), response_body, created_at, updated_at
		FROM idempotency_records WHERE idempotency_key = $1`
	rec := &domain.IdempotencyRecord{}
	err := r.db.QueryRowContext(ctx, q, key).Scan(
		&rec.IdempotencyKey, &rec.RequestHash, &rec.Status, &rec.TransferID,
		&rec.ResponseStatus, &rec.ResponseBody, &rec.CreatedAt, &rec.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}
	return rec, nil
}
