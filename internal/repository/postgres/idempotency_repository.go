package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lib/pq"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository"
)

type IdempotencyRepository struct{}

func NewIdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{}
}

// pqUniqueViolation is Postgres' error code for a unique constraint violation.
const pqUniqueViolation = "23505"

// ErrKeyAlreadyExists signals the idempotency key was already inserted —
// either a genuine concurrent duplicate or a completed prior request.
var ErrKeyAlreadyExists = errors.New("idempotency key already exists")

// Insert reserves the idempotency key as IN_PROGRESS. A unique constraint
// violation here is the mechanism that catches concurrent duplicate requests.
func (r *IdempotencyRepository) Insert(ctx context.Context, q repository.Querier, key, fingerprint string) (*domain.IdempotencyRecord, error) {
	row := q.QueryRowContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_fingerprint, status)
		VALUES ($1, $2, $3)
		RETURNING idempotency_key, request_fingerprint, status, created_at, updated_at
	`, key, fingerprint, domain.IdempotencyStatusInProgress)

	var rec domain.IdempotencyRecord
	if err := row.Scan(&rec.IdempotencyKey, &rec.RequestFingerprint, &rec.Status, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == pqUniqueViolation {
			return nil, ErrKeyAlreadyExists
		}
		return nil, fmt.Errorf("insert idempotency record: %w", err)
	}

	return &rec, nil
}

func (r *IdempotencyRepository) GetByKey(ctx context.Context, q repository.Querier, key string) (*domain.IdempotencyRecord, error) {
	row := q.QueryRowContext(ctx, `
		SELECT idempotency_key, request_fingerprint, transfer_id, status,
		       response_snapshot, created_at, updated_at
		FROM idempotency_records
		WHERE idempotency_key = $1
	`, key)

	var rec domain.IdempotencyRecord
	var transferID sql.NullString
	var snapshot sql.NullString

	if err := row.Scan(&rec.IdempotencyKey, &rec.RequestFingerprint, &transferID, &rec.Status,
		&snapshot, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("scan idempotency record: %w", err)
	}

	if transferID.Valid {
		rec.TransferID = &transferID.String
	}
	if snapshot.Valid {
		rec.ResponseSnapshot = []byte(snapshot.String)
	}

	return &rec, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, q repository.Querier, key string, transferID string, responseSnapshot []byte) error {
	_, err := q.ExecContext(ctx, `
		UPDATE idempotency_records
		SET status = $1, transfer_id = $2, response_snapshot = $3, updated_at = now()
		WHERE idempotency_key = $4
	`, domain.IdempotencyStatusCompleted, transferID, responseSnapshot, key)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}

	return nil
}
