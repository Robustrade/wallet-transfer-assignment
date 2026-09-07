package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

// IdempotencyRepository persists idempotency keys. The primary key on
// idempotency_records.key is the durable exactly-once guarantee.
type IdempotencyRepository struct{ *DB }

// NewIdempotencyRepository wires the repository to a DB.
func NewIdempotencyRepository(db *DB) *IdempotencyRepository { return &IdempotencyRepository{DB: db} }

// Insert claims the key. A unique-constraint violation is reported as
// domain.ErrDuplicateIdempotencyKey so the service can replay the original
// result.
func (r *IdempotencyRepository) Insert(ctx context.Context, rec service.IdempotencyRecord) error {
	_, err := r.q(ctx).Exec(ctx,
		`INSERT INTO idempotency_records (key, transfer_id, request_hash)
		 VALUES ($1, $2, $3)`,
		rec.Key, rec.TransferID, rec.RequestHash)
	if err != nil {
		if isUniqueViolation(err, "") {
			return domain.ErrDuplicateIdempotencyKey
		}
		return fmt.Errorf("postgres: insert idempotency record: %w", err)
	}
	return nil
}

// GetRecord loads the record for a key.
func (r *IdempotencyRepository) GetRecord(ctx context.Context, key string) (service.IdempotencyRecord, error) {
	var rec service.IdempotencyRecord
	err := r.q(ctx).QueryRow(ctx,
		`SELECT key, transfer_id, request_hash FROM idempotency_records WHERE key = $1`, key).
		Scan(&rec.Key, &rec.TransferID, &rec.RequestHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return service.IdempotencyRecord{}, domain.ErrTransferNotFound
		}
		return service.IdempotencyRecord{}, fmt.Errorf("postgres: scan idempotency record: %w", err)
	}
	return rec, nil
}
