package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const pgErrCodeQueryCanceled = "57014"

type IdempotencyStatus string

const (
	IdempotencyInProgress IdempotencyStatus = "IN_PROGRESS"
	IdempotencyCompleted  IdempotencyStatus = "COMPLETED"
)

type IdempotencyRecord struct {
	Key                string
	RequestFingerprint string
	Status             IdempotencyStatus
	TransferID         *uuid.UUID
	ResponseStatus     *int
	ResponseBody       json.RawMessage
}

type IdempotencyRepo struct{}

func NewIdempotencyRepo() *IdempotencyRepo {
	return &IdempotencyRepo{}
}

// TryInsert attempts to claim the idempotency key. It returns true if this
// call created the record (i.e. this request is the one that should
// perform the transfer), or false if a record already existed.
func (r *IdempotencyRepo) TryInsert(ctx context.Context, q Querier, key, fingerprint string) (bool, error) {
	res, err := q.ExecContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_fingerprint)
		VALUES ($1, $2)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, key, fingerprint)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ErrWaitTimeout is returned when LockForUpdate could not acquire the row
// lock within the given timeout because another request is still holding
// it (the original request is still mid-transaction).
var ErrWaitTimeout = errors.New("timed out waiting for in-flight duplicate request to complete")

// LockForUpdate reads the idempotency record while holding its row lock.
// If another transaction currently holds the lock (an in-flight duplicate
// request working the same key), this blocks until that transaction
// commits or rolls back, up to timeout. This is what gives duplicate
// requests "wait for the original to finish" semantics without any
// application-level polling: Postgres's row lock does the waiting.
func (r *IdempotencyRepo) LockForUpdate(ctx context.Context, tx *sql.Tx, key string, timeout time.Duration) (*IdempotencyRecord, error) {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", timeout.Milliseconds())); err != nil {
		return nil, err
	}

	var rec IdempotencyRecord
	var responseBody []byte
	err := tx.QueryRowContext(ctx, `
		SELECT idempotency_key, request_fingerprint, status, transfer_id, response_status, response_body
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE
	`, key).Scan(&rec.Key, &rec.RequestFingerprint, &rec.Status, &rec.TransferID, &rec.ResponseStatus, &responseBody)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrCodeQueryCanceled {
			return nil, ErrWaitTimeout
		}
		return nil, err
	}

	// The lock is acquired: reset statement_timeout so it doesn't keep
	// applying to the rest of this transaction (ledger inserts, wallet
	// updates), which could otherwise get canceled under load for an
	// unrelated reason.
	if _, err := tx.ExecContext(ctx, "SET LOCAL statement_timeout = DEFAULT"); err != nil {
		return nil, err
	}

	rec.ResponseBody = responseBody
	return &rec, nil
}

func (r *IdempotencyRepo) Complete(ctx context.Context, q Querier, key string, transferID *uuid.UUID, responseStatus int, responseBody []byte) error {
	_, err := q.ExecContext(ctx, `
		UPDATE idempotency_records
		SET status = $2, transfer_id = $3, response_status = $4, response_body = $5, updated_at = now()
		WHERE idempotency_key = $1
	`, key, IdempotencyCompleted, transferID, responseStatus, responseBody)
	return err
}
