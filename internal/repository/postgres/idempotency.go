package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

const idempotencyColumns = `
	idempotency_key, transfer_id, request_hash, response_status, response_body, created_at
`

// IdempotencyRepository implements repository.IdempotencyRepository.
type IdempotencyRepository struct {
	db *sql.DB
}

// GetByKey loads an idempotency record by key.
func (r *IdempotencyRepository) GetByKey(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	exec := dbOrTx(ctx, r.db)
	query := `SELECT ` + idempotencyColumns + ` FROM idempotency_records WHERE idempotency_key = $1`

	var record domain.IdempotencyRecord
	var transferID sql.NullString
	row := exec.QueryRowContext(ctx, query, key)
	err := row.Scan(
		&record.Key,
		&transferID,
		&record.RequestHash,
		&record.ResponseStatus,
		&record.ResponseBody,
		&record.CreatedAt,
	)
	if err != nil {
		return nil, mapRowError(err, "idempotency record")
	}
	if transferID.Valid {
		record.TransferID = transferID.String
	}
	return &record, nil
}

// Save inserts an idempotency record.
func (r *IdempotencyRepository) Save(ctx context.Context, record *domain.IdempotencyRecord) error {
	exec := dbOrTx(ctx, r.db)
	const query = `
		INSERT INTO idempotency_records (
			idempotency_key,
			transfer_id,
			request_hash,
			response_status,
			response_body
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`

	var transferID any
	if record.TransferID != "" {
		transferID = record.TransferID
	}

	row := exec.QueryRowContext(
		ctx,
		query,
		record.Key,
		transferID,
		record.RequestHash,
		record.ResponseStatus,
		record.ResponseBody,
	)
	if err := row.Scan(&record.CreatedAt); err != nil {
		return fmt.Errorf("insert idempotency record: %w", err)
	}
	return nil
}
