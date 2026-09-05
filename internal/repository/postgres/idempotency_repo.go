package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallet-transfer/internal/domain"
)

type IdempotencyRepository struct {
	tx *TxManager
}

func NewIdempotencyRepository(tx *TxManager) *IdempotencyRepository {
	return &IdempotencyRepository{tx: tx}
}

func (r *IdempotencyRepository) Get(ctx context.Context, key string) (domain.IdempotencyRecord, error) {
	const query = `
		SELECT idempotency_key, request_fingerprint, transfer_id, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1`

	var record domain.IdempotencyRecord
	err := r.tx.executorFrom(ctx).QueryRowContext(ctx, query, key).
		Scan(&record.Key, &record.Fingerprint, &record.TransferID, &record.ResponseBody)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return domain.IdempotencyRecord{}, domain.ErrIdempotencyRecordNotFound
	case err != nil:
		return domain.IdempotencyRecord{}, fmt.Errorf("get idempotency record %s: %w", key, translateError(err))
	}
	return record, nil
}

func (r *IdempotencyRepository) Create(ctx context.Context, record domain.IdempotencyRecord) error {
	const query = `
		INSERT INTO idempotency_records (idempotency_key, request_fingerprint, transfer_id, response_body)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (idempotency_key) DO NOTHING`

	result, err := r.tx.executorFrom(ctx).ExecContext(ctx, query,
		record.Key, record.Fingerprint, record.TransferID, []byte(record.ResponseBody))
	if err != nil {
		return fmt.Errorf("create idempotency record %s: %w", record.Key, translateError(err))
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("create idempotency record %s: %w", record.Key, translateError(err))
	}
	if affected == 0 {
		return domain.ErrIdempotencyKeyAlreadyUsed
	}
	return nil
}
