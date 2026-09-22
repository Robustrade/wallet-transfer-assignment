package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

type idempotencyRepo struct {
	db               dbtx
	maxClaimAttempts int
}

func (r *idempotencyRepo) Claim(ctx context.Context, key, requestHash string) (bool, *domain.IdempotencyRecord, error) {
	for attempt := 0; attempt < r.maxClaimAttempts; attempt++ {
		var inserted string
		err := r.db.QueryRow(ctx, `
			INSERT INTO idempotency_records (key, request_hash, created_at)
			VALUES ($1, $2, now())
			ON CONFLICT (key) DO NOTHING
			RETURNING key`, key, requestHash).Scan(&inserted)
		if err == nil {
			return true, nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, nil, err
		}

		// Someone else holds the key (or held it and rolled back). Look at
		// the current row, taking a row lock so that if the holder is
		// still mid-transaction we block until it commits or rolls back
		// rather than racing it.
		existing, err := r.getForUpdate(ctx, key)
		if errors.Is(err, domain.ErrTransferNotFound) {
			// The other transaction rolled back after our insert attempt
			// failed; retry the insert now that the row is gone.
			continue
		}
		if err != nil {
			return false, nil, err
		}
		return false, existing, nil
	}
	return false, nil, fmt.Errorf("claim idempotency key %q: exceeded retry attempts", key)
}

func (r *idempotencyRepo) getForUpdate(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	row := r.db.QueryRow(ctx, `
		SELECT key, request_hash, COALESCE(transfer_id, ''), COALESCE(response_status, 0), COALESCE(response_body, ''::bytea), created_at, completed_at
		FROM idempotency_records WHERE key = $1 FOR UPDATE`, key)

	var rec domain.IdempotencyRecord
	err := row.Scan(&rec.Key, &rec.RequestHash, &rec.TransferID, &rec.ResponseStatus, &rec.ResponseBody, &rec.CreatedAt, &rec.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Reuse ErrTransferNotFound purely as a "no row" sentinel for the
		// retry loop above; it is never surfaced to callers of Claim.
		return nil, domain.ErrTransferNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *idempotencyRepo) Complete(ctx context.Context, key, transferID string, responseStatus int, responseBody []byte) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE idempotency_records
		SET transfer_id = $2, response_status = $3, response_body = $4, completed_at = now()
		WHERE key = $1`,
		key, transferID, responseStatus, responseBody)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("complete idempotency key %q: no such claim", key)
	}
	return nil
}
