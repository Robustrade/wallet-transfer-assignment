package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

// IdempotencyRepository is the PostgreSQL adapter for the durable dedup store.
type IdempotencyRepository struct {
	db *DB
}

// NewIdempotencyRepository constructs an IdempotencyRepository over db.
func NewIdempotencyRepository(db *DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

var _ service.IdempotencyRepository = (*IdempotencyRepository)(nil)

// Insert claims an idempotency key. A unique-constraint violation on the
// primary key is mapped to domain.ErrDuplicateIdempotencyKey so the service can
// switch to replay.
func (r *IdempotencyRepository) Insert(ctx context.Context, record *domain.IdempotencyRecord) error {
	var transferID *string
	if record.TransferID != "" {
		transferID = &record.TransferID
	}
	var responseBody []byte
	if len(record.ResponseBody) > 0 {
		responseBody = record.ResponseBody
	}

	_, err := r.db.conn(ctx).Exec(ctx,
		`INSERT INTO idempotency_records
		   (key, request_hash, transfer_id, response_body, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now(), now())`,
		record.Key,
		record.RequestHash,
		transferID,
		responseBody,
		string(record.Status),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("postgres: insert idempotency record: %w", err)
	}
	return nil
}

// Get returns the record for a key. Absence is signalled with pgx.ErrNoRows;
// the service only calls Get after observing a duplicate, so a missing row is
// an unexpected condition there.
func (r *IdempotencyRepository) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	var (
		rec          domain.IdempotencyRecord
		status       string
		transferID   *string
		responseBody []byte
	)
	err := r.db.conn(ctx).QueryRow(ctx,
		`SELECT key, request_hash, transfer_id, response_body, status, created_at, updated_at
		   FROM idempotency_records WHERE key = $1`,
		key,
	).Scan(
		&rec.Key,
		&rec.RequestHash,
		&transferID,
		&responseBody,
		&status,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pgx.ErrNoRows
		}
		return nil, fmt.Errorf("postgres: get idempotency record: %w", err)
	}
	if transferID != nil {
		rec.TransferID = *transferID
	}
	rec.ResponseBody = responseBody
	rec.Status = domain.IdempotencyStatus(status)
	return &rec, nil
}

// Complete finalises a record with the produced transfer id and the serialised
// response replayed for future duplicates.
func (r *IdempotencyRepository) Complete(ctx context.Context, key, transferID string, response []byte) error {
	var tid *string
	if transferID != "" {
		tid = &transferID
	}
	tag, err := r.db.conn(ctx).Exec(ctx,
		`UPDATE idempotency_records
		    SET status = $2, transfer_id = $3, response_body = $4, updated_at = now()
		  WHERE key = $1`,
		key,
		string(domain.IdempotencyCompleted),
		tid,
		response,
	)
	if err != nil {
		return fmt.Errorf("postgres: complete idempotency record: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
