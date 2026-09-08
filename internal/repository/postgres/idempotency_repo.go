package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallettransfer/internal/domain"
)

// IdempotencyRepo is the PostgreSQL implementation of service.IdempotencyRepo.
//
// Claim relies on PostgreSQL's documented behavior for
// INSERT ... ON CONFLICT: if a concurrent transaction is inserting a
// conflicting row, the second inserter blocks until the first transaction
// finishes, and only then evaluates the ON CONFLICT clause. This means:
//
//   - If the first transaction commits, the second sees the conflict, does
//     nothing, and can then safely SELECT the now-committed, fully-populated
//     row (including the COMPLETED status and stored response).
//   - If the first transaction rolls back, there is no conflict at all —
//     the second transaction's INSERT proceeds normally and it becomes the
//     new claimant.
//
// This gives us correct exactly-once behavior under real concurrent
// duplicate requests without any manual SELECT ... FOR UPDATE polling loop.
type IdempotencyRepo struct {
	db *sql.DB
}

func NewIdempotencyRepo(db *sql.DB) *IdempotencyRepo { return &IdempotencyRepo{db: db} }

func (r *IdempotencyRepo) Claim(ctx context.Context, tx *sql.Tx, key, requestFingerprint string) (domain.IdempotencyRecord, bool, error) {
	const insertQ = `
		INSERT INTO idempotency_records (idempotency_key, request_fingerprint, status, created_at, updated_at)
		VALUES ($1, $2, 'IN_PROGRESS', now(), now())
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key, request_fingerprint, status, transfer_id, response_status, response_body, created_at, updated_at`

	var rec domain.IdempotencyRecord
	var transferID sql.NullString
	var respStatus sql.NullInt64
	var respBody []byte

	err := tx.QueryRowContext(ctx, insertQ, key, requestFingerprint).Scan(
		&rec.Key, &rec.RequestFingerprint, &rec.Status, &transferID, &respStatus, &respBody, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err == nil {
		return domain.IdempotencyRecord{}, true, nil // fresh claim, caller proceeds
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return domain.IdempotencyRecord{}, false, fmt.Errorf("claim idempotency key %s: %w", key, err)
	}

	// Conflict: a record already exists (and, per the semantics above, is
	// now guaranteed to reflect a committed — or absent — prior attempt).
	const selectQ = `
		SELECT idempotency_key, request_fingerprint, status, transfer_id, response_status, response_body, created_at, updated_at
		FROM idempotency_records WHERE idempotency_key = $1`
	err = tx.QueryRowContext(ctx, selectQ, key).Scan(
		&rec.Key, &rec.RequestFingerprint, &rec.Status, &transferID, &respStatus, &respBody, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if err != nil {
		return domain.IdempotencyRecord{}, false, fmt.Errorf("read existing idempotency record %s: %w", key, err)
	}
	if transferID.Valid {
		rec.TransferID = transferID.String
	}
	if respStatus.Valid {
		rec.ResponseStatus = int(respStatus.Int64)
	}
	rec.ResponseBody = respBody
	return rec, false, nil
}

func (r *IdempotencyRepo) Complete(ctx context.Context, tx *sql.Tx, key string, transferID string, responseStatus int, responseBody []byte) error {
	const q = `
		UPDATE idempotency_records
		SET status = 'COMPLETED', transfer_id = $1, response_status = $2, response_body = $3, updated_at = now()
		WHERE idempotency_key = $4 AND status = 'IN_PROGRESS'`
	// transfer_id is nullable: validation-stage failures (e.g. unknown
	// wallet) never create a transfers row, since that row's foreign keys
	// would have nothing valid to reference.
	var transferIDArg any
	if transferID != "" {
		transferIDArg = transferID
	}
	res, err := tx.ExecContext(ctx, q, transferIDArg, responseStatus, string(responseBody), key)
	if err != nil {
		return fmt.Errorf("complete idempotency record %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("complete idempotency record %s: no such record", key)
	}
	return nil
}
