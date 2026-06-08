package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

// IdempotencyRecord is the persisted state for a single idempotency key.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	TransferID     *uuid.UUID
	ResponseStatus int
	ResponseBody   json.RawMessage
	CreatedAt      time.Time
}

type IdempotencyRepository struct{}

// Insert attempts to create a new idempotency record.
// Returns (record, true, nil) if the row was inserted (new key).
// Returns (existing, false, nil) if the key already existed (conflict).
func (r *IdempotencyRepository) Insert(ctx context.Context, tx pgx.Tx, key, requestHash string) (*IdempotencyRecord, bool, error) {
	const q = `
		INSERT INTO idempotency_records (key, request_hash, response_status, response_body)
		VALUES ($1, $2, 0, '{}')
		ON CONFLICT (key) DO NOTHING
		RETURNING key, request_hash, transfer_id, response_status, response_body, created_at`

	row := tx.QueryRow(ctx, q, key, requestHash)
	rec, err := scanIdempotencyRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Conflict — fetch the existing record.
			existing, fetchErr := r.GetByKey(ctx, tx, key)
			if fetchErr != nil {
				return nil, false, fetchErr
			}
			return existing, false, nil
		}
		return nil, false, fmt.Errorf("IdempotencyRepository.Insert: %w", err)
	}
	return rec, true, nil
}

// GetByKey fetches an existing idempotency record by key.
func (r *IdempotencyRepository) GetByKey(ctx context.Context, tx pgx.Tx, key string) (*IdempotencyRecord, error) {
	const q = `
		SELECT key, request_hash, transfer_id, response_status, response_body, created_at
		FROM idempotency_records
		WHERE key = $1`

	row := tx.QueryRow(ctx, q, key)
	rec, err := scanIdempotencyRecord(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// This should never happen: we just observed a conflict on this key.
			return nil, fmt.Errorf("idempotency record vanished after conflict: %w", err)
		}
		return nil, fmt.Errorf("IdempotencyRepository.GetByKey: %w", err)
	}
	return rec, nil
}

// UpdateResponse stores the final response and transfer ID once the transfer completes.
func (r *IdempotencyRepository) UpdateResponse(
	ctx context.Context, tx pgx.Tx,
	key string, transferID uuid.UUID, status int, body json.RawMessage,
) error {
	const q = `
		UPDATE idempotency_records
		SET transfer_id = $2, response_status = $3, response_body = $4
		WHERE key = $1`

	tag, err := tx.Exec(ctx, q,
		key,
		pgtype.UUID{Bytes: transferID, Valid: true},
		status,
		body,
	)
	if err != nil {
		return fmt.Errorf("IdempotencyRepository.UpdateResponse: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("idempotency record %q vanished before UpdateResponse: %w", key, domain.ErrInvariantViolation)
	}
	return nil
}

func scanIdempotencyRecord(row pgx.Row) (*IdempotencyRecord, error) {
	var (
		rec          IdempotencyRecord
		pgTransferID pgtype.UUID
		rawBody      []byte
	)
	err := row.Scan(
		&rec.Key,
		&rec.RequestHash,
		&pgTransferID,
		&rec.ResponseStatus,
		&rawBody,
		&rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if pgTransferID.Valid {
		id := uuid.UUID(pgTransferID.Bytes)
		rec.TransferID = &id
	}
	rec.ResponseBody = json.RawMessage(rawBody)
	return &rec, nil
}
