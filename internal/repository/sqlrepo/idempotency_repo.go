package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

type idempotencyRepo struct {
	q Querier
	d Dialect
}

// Insert claims the key with ON CONFLICT DO NOTHING, which never errors on a
// duplicate. inserted reports whether this call created the row.
//
// Concurrency: if another open transaction has already inserted the same key,
// the database makes this statement wait on the primary key (Postgres), or
// this whole transaction is already queued behind it (SQLite, BEGIN IMMEDIATE).
// Once that transaction ends, a commit makes this a no-op
// (inserted = false, so the caller replays), and a rollback lets this insert
// succeed.
//
// "key" is quoted because KEY is a keyword in both databases.
func (r *idempotencyRepo) Insert(ctx context.Context, key, requestHash string, createdAt time.Time) (bool, error) {
	res, err := r.q.ExecContext(ctx, r.d.Rebind(
		`INSERT INTO idempotency_records ("key", request_hash, transfer_id, created_at)
		 VALUES (?, ?, NULL, ?)
		 ON CONFLICT ("key") DO NOTHING`),
		key, requestHash, FormatTime(createdAt))
	if err != nil {
		return false, fmt.Errorf("insert idempotency record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert idempotency record: %w", err)
	}
	return n == 1, nil
}

func (r *idempotencyRepo) Get(ctx context.Context, key string) (*service.IdempotencyRecord, error) {
	var rec service.IdempotencyRecord
	err := r.q.QueryRowContext(ctx, r.d.Rebind(
		`SELECT "key", request_hash, transfer_id, created_at FROM idempotency_records WHERE "key" = ?`), key).
		Scan(&rec.Key, &rec.RequestHash, &rec.TransferID, scanTime(&rec.CreatedAt))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("idempotency record: %w", service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get idempotency record: %w", err)
	}
	return &rec, nil
}

// LinkTransfer sets transfer_id only while it is still NULL. A key can
// therefore never be re-pointed at a different transfer once linked.
func (r *idempotencyRepo) LinkTransfer(ctx context.Context, key string, transferID uuid.UUID) error {
	res, err := r.q.ExecContext(ctx, r.d.Rebind(
		`UPDATE idempotency_records SET transfer_id = ? WHERE "key" = ? AND transfer_id IS NULL`),
		transferID, key)
	if err != nil {
		return fmt.Errorf("link idempotency record to transfer %s: %w", transferID, err)
	}
	return expectOneRow(res, fmt.Errorf("link idempotency record to transfer %s: record missing or already linked", transferID))
}
