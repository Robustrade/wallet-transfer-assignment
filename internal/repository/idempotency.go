package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
)

// IdempotencyRecord links an idempotency key to the transfer it created.
type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  string
	// Result is the JSON snapshot of the final transfer. Nil while the transfer is PENDING.
	Result []byte
}

// IdempotencyRepository persists idempotency records.
type IdempotencyRepository struct{}

// TryInsert stores the key and returns false if the key already exists.
// If another transaction is inserting the same key right now, Postgres makes this call
// wait on the primary key until that transaction ends, so only one insert can win.
func (IdempotencyRepository) TryInsert(ctx context.Context, q db.Querier, key, requestHash, transferID string) (bool, error) {
	res, err := q.ExecContext(ctx,
		`INSERT INTO idempotency_records (idempotency_key, request_hash, transfer_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (idempotency_key) DO NOTHING`, key, requestHash, transferID)
	if err != nil {
		return false, fmt.Errorf("insert idempotency record: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n == 1, nil
}

// FindByKey returns (nil, nil) if the key is unknown.
func (IdempotencyRepository) FindByKey(ctx context.Context, q db.Querier, key string) (*IdempotencyRecord, error) {
	return findRecord(q.QueryRowContext(ctx,
		`SELECT idempotency_key, request_hash, transfer_id, result
		   FROM idempotency_records WHERE idempotency_key = $1`, key))
}

// FindByTransferID returns (nil, nil) if no record points at the transfer.
func (IdempotencyRepository) FindByTransferID(ctx context.Context, q db.Querier, transferID string) (*IdempotencyRecord, error) {
	return findRecord(q.QueryRowContext(ctx,
		`SELECT idempotency_key, request_hash, transfer_id, result
		   FROM idempotency_records WHERE transfer_id = $1`, transferID))
}

func findRecord(row *sql.Row) (*IdempotencyRecord, error) {
	var r IdempotencyRecord
	err := row.Scan(&r.Key, &r.RequestHash, &r.TransferID, &r.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find idempotency record: %w", err)
	}
	return &r, nil
}

// SaveResult stores the final result snapshot for the transfer.
func (IdempotencyRepository) SaveResult(ctx context.Context, q db.Querier, transferID string, result []byte) error {
	if _, err := q.ExecContext(ctx,
		`UPDATE idempotency_records SET result = $2, updated_at = now() WHERE transfer_id = $1`,
		transferID, string(result)); err != nil { // string: lib/pq would send []byte as bytea
		return fmt.Errorf("save idempotency result: %w", err)
	}
	return nil
}
