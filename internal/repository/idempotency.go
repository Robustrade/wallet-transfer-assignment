package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")

type IdempotencyRecord struct {
	Key string
	RequestHash string
	TransferID string
	ResponseStatus int
	ResponseBody []byte
}

type IdempotencyRepository struct { db DBTX }

func NewIdempotencyRepository(db DBTX) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// Claim returns true only to the transaction that inserted the key. PostgreSQL
// waits on an uncommitted conflicting unique key before returning no row.
func (r *IdempotencyRepository) Claim(ctx context.Context, key, requestHash string) (bool, error) {
	const query = `
		INSERT INTO idempotency_records (idempotency_key, request_hash)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key`
	var inserted string
	err := r.db.QueryRowContext(ctx, query, key, requestHash).Scan(&inserted)
	if errors.Is(err, sql.ErrNoRows) { return false, nil }
	if err != nil { return false, fmt.Errorf("insert idempotency claim: %w", err) }
	return true, nil
}

func (r *IdempotencyRepository) Get(ctx context.Context, key string) (IdempotencyRecord, error) {
	const query = `
		SELECT idempotency_key, request_hash, COALESCE(transfer_id, ''),
		       COALESCE(response_status, 0), response_body
		FROM idempotency_records WHERE idempotency_key = $1`
	var record IdempotencyRecord
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&record.Key, &record.RequestHash, &record.TransferID, &record.ResponseStatus, &record.ResponseBody)
	if errors.Is(err, sql.ErrNoRows) { return IdempotencyRecord{}, fmt.Errorf("%w: %s", ErrIdempotencyRecordNotFound, key) }
	if err != nil { return IdempotencyRecord{}, fmt.Errorf("get idempotency record: %w", err) }
	return record, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, key, requestHash, transferID string, status int, body []byte) error {
	const query = `
		UPDATE idempotency_records
		SET transfer_id = $3, response_status = $4, response_body = $5::jsonb
		WHERE idempotency_key = $1 AND request_hash = $2
		  AND response_status IS NULL AND response_body IS NULL`
	result, err := execContext(r.db, ctx, query, key, requestHash, transferID, status, string(body))
	if err != nil { return fmt.Errorf("complete idempotency record: %w", err) }
	rows, err := result.RowsAffected()
	if err != nil { return fmt.Errorf("read idempotency update count: %w", err) }
	if rows != 1 { return fmt.Errorf("complete idempotency record: expected one pending row, updated %d", rows) }
	return nil
}

// execContext is kept as a narrow optional capability so the shared DBTX
// interface remains sufficient for repositories that only read/return rows.
func execContext(db DBTX, ctx context.Context, query string, args ...any) (sql.Result, error) {
	execer, ok := db.(interface { ExecContext(context.Context, string, ...any) (sql.Result, error) })
	if !ok { return nil, errors.New("database executor does not support ExecContext") }
	return execer.ExecContext(ctx, query, args...)
}
