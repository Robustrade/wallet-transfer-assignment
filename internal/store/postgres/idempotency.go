package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"wallet/internal/constants"
	"wallet/internal/domain"
)

// ClaimIdempotency implements service.Tx.
func (t *txStore) ClaimIdempotency(ctx context.Context, key string, canonicalRequest []byte) (bool, error) {
	tag, err := t.tx.Exec(ctx, constants.ClaimIdempotencySQL, key, canonicalRequest)
	if err != nil {
		return false, mapError(fmt.Errorf("claim idempotency key: %w", err))
	}
	return tag.RowsAffected() == 1, nil
}

// CompleteIdempotency implements service.Tx.
func (t *txStore) CompleteIdempotency(ctx context.Context, key string, transferID *string, status int, body []byte) error {
	tag, err := t.tx.Exec(ctx, constants.CompleteIdempotencySQL, key, transferID, status, string(body))
	if err != nil {
		return mapError(fmt.Errorf("complete idempotency record: %w", err))
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("complete idempotency record: updated %d rows, want exactly 1", tag.RowsAffected())
	}
	return nil
}

// GetIdempotencyRecord implements both service.Tx and service.Store.
func (t *txStore) GetIdempotencyRecord(ctx context.Context, key string, canonicalRequest []byte) (domain.IdempotencyRecord, bool, error) {
	return getRecord(ctx, t.tx, key, canonicalRequest)
}

// GetIdempotencyRecord implements service.Store. It runs on its own
// connection and is used to resolve an unknown commit outcome.
func (s *Store) GetIdempotencyRecord(ctx context.Context, key string, canonicalRequest []byte) (domain.IdempotencyRecord, bool, error) {
	ctx, cancel := boundedPoolCtx(ctx)
	defer cancel()
	return getRecord(ctx, s.pool, key, canonicalRequest)
}

func getRecord(ctx context.Context, q querier, key string, canonicalRequest []byte) (domain.IdempotencyRecord, bool, error) {
	rec := domain.IdempotencyRecord{Key: key}

	var (
		transferID     *string
		responseStatus *int32
		responseBody   *string
	)
	err := q.QueryRow(ctx, constants.GetIdempotencyRecordSQL, key, canonicalRequest).
		Scan(&rec.RequestMatches, &transferID, &responseStatus, &responseBody)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.IdempotencyRecord{}, false, nil
	}
	if err != nil {
		return domain.IdempotencyRecord{}, false, mapError(fmt.Errorf("get idempotency record: %w", err))
	}

	rec.TransferID = transferID
	if responseStatus != nil {
		status := int(*responseStatus)
		rec.ResponseStatus = &status
	}
	if responseBody != nil {
		rec.ResponseBody = []byte(*responseBody)
	}
	return rec, true, nil
}
