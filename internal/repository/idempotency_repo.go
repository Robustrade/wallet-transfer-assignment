package repository

import (
	"context"
	"errors"
	"hash/fnv"

	"github.com/jackc/pgx/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// IdempotencyRepository handles idempotency record persistence.
// The idempotency_records table acts as a gate before the transfers table,
// preventing duplicate processing and detecting mismatched duplicate requests.
type IdempotencyRepository struct{}

// NewIdempotencyRepository creates a new IdempotencyRepository.
func NewIdempotencyRepository() *IdempotencyRepository {
	return &IdempotencyRepository{}
}

// Create attempts to insert a new idempotency record using INSERT ... ON CONFLICT DO NOTHING.
// Returns true if the record was created (key is new), false if it already exists (duplicate).
// When called within a transaction, a concurrent INSERT with the same key will block
// until this transaction commits or rolls back — providing serialization of duplicate requests.
func (r *IdempotencyRepository) Create(ctx context.Context, db DBTX, record *domain.IdempotencyRecord) (bool, error) {
	result, err := db.Exec(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (idempotency_key) DO NOTHING`,
		record.IdempotencyKey, record.RequestHash, record.Status,
	)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

// GetByKey retrieves an idempotency record by its key. Returns nil (not an error) if not found.
func (r *IdempotencyRepository) GetByKey(ctx context.Context, db DBTX, key string) (*domain.IdempotencyRecord, error) {
	record := &domain.IdempotencyRecord{}
	err := db.QueryRow(ctx,
		`SELECT idempotency_key, request_hash, transfer_id::text, status,
		        created_at, updated_at
		 FROM idempotency_records WHERE idempotency_key = $1`,
		key,
	).Scan(
		&record.IdempotencyKey, &record.RequestHash, &record.TransferID,
		&record.Status, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

// MarkCompleted updates an idempotency record to COMPLETED status with the transfer ID.
func (r *IdempotencyRepository) MarkCompleted(ctx context.Context, db DBTX, key string, transferID string) error {
	result, err := db.Exec(ctx,
		`UPDATE idempotency_records
		 SET status = $1, transfer_id = $2::uuid, updated_at = NOW()
		 WHERE idempotency_key = $3`,
		domain.IdempotencyStatusCompleted, transferID, key,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrIdempotencyKeyNotFound
	}
	return nil
}

// MarkFailed updates an idempotency record to FAILED status with an optional transfer ID.
func (r *IdempotencyRepository) MarkFailed(ctx context.Context, db DBTX, key string, transferID *string) error {
	result, err := db.Exec(ctx,
		`UPDATE idempotency_records
		 SET status = $1, transfer_id = $2::uuid, updated_at = NOW()
		 WHERE idempotency_key = $3`,
		domain.IdempotencyStatusFailed, transferID, key,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return domain.ErrIdempotencyKeyNotFound
	}
	return nil
}

// TryAdvisoryXactLock attempts to acquire a transaction-level advisory lock on the idempotency key.
// It returns true if the lock was successfully acquired, or false if it is held by another transaction.
func (r *IdempotencyRepository) TryAdvisoryXactLock(ctx context.Context, db DBTX, key string) (bool, error) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	lockID := int64(h.Sum64())

	var acquired bool
	err := db.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock($1)", lockID).Scan(&acquired)
	if err != nil {
		return false, err
	}
	return acquired, nil
}
