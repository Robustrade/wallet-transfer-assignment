package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/repository"
)

func (t *transaction) ReserveIdempotencyKey(ctx context.Context, key, fingerprint, transferID string, at time.Time) (bool, error) {
	if t.failure != nil {
		return false, t.failure
	}
	var claimed string
	err := t.tx.QueryRowContext(ctx, `INSERT INTO idempotency_records (idempotency_key,request_fingerprint,transfer_id,created_at,updated_at) VALUES ($1,$2,$3,$4,$4) ON CONFLICT (idempotency_key) DO NOTHING RETURNING idempotency_key`, key, fingerprint, transferID, at).Scan(&claimed)
	// No returned row is the expected duplicate outcome, not a statement error.
	// At READ COMMITTED, read the winner in a separate statement/snapshot.
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, t.record("reserve idempotency key", err)
	}
	return true, nil
}
func (t *transaction) GetIdempotencyRecord(ctx context.Context, key string) (repository.IdempotencyRecord, error) {
	var v repository.IdempotencyRecord
	if t.failure != nil {
		return v, t.failure
	}
	var status sql.NullInt64
	var body sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT idempotency_key,request_fingerprint,transfer_id,response_status,response_body,created_at,updated_at FROM idempotency_records WHERE idempotency_key=$1`, key).Scan(&v.Key, &v.Fingerprint, &v.TransferID, &status, &body, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		return v, t.record("get idempotency record", err)
	}
	if status.Valid != body.Valid {
		return v, t.record("decode idempotency result", fmt.Errorf("incomplete stored result"))
	}
	if status.Valid {
		v.Result = &repository.Result{StatusCode: int(status.Int64), Body: body.String}
	}
	return v, nil
}
func (t *transaction) CompleteIdempotencyKey(ctx context.Context, key, transferID string, result repository.Result, at time.Time) error {
	if result.StatusCode < 100 || result.StatusCode > 599 {
		return fmt.Errorf("invalid result status code")
	}
	return t.update(ctx, "complete idempotency key", `UPDATE idempotency_records SET response_status=$3,response_body=$4,updated_at=$5 WHERE idempotency_key=$1 AND transfer_id=$2 AND response_status IS NULL AND response_body IS NULL`, repository.ErrConflict, key, transferID, result.StatusCode, result.Body, at)
}
