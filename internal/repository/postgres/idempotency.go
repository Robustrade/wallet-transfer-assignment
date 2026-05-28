package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// pgUniqueViolation is the SQLSTATE code Postgres returns when a unique
// constraint is violated. It's the cornerstone of the idempotency
// strategy: concurrent first-time inserts of the same key block on the
// PK lock; the loser receives this code; the service translates it to
// repository.ErrIdempotencyExists and replays the winner's response.
const pgUniqueViolation = "23505"

// IdempotencyRepo is the PostgreSQL implementation of
// repository.IdempotencyRepository.
type IdempotencyRepo struct{}

func NewIdempotencyRepo() *IdempotencyRepo { return &IdempotencyRepo{} }

// Get returns the idempotency record for key, or
// repository.ErrNotFound. Used both by the service's fast-path replay
// (outside any transaction) and by the test suite.
func (r *IdempotencyRepo) Get(ctx context.Context, exec repository.Executor, key string) (*repository.IdempotencyRecord, error) {
	const q = `SELECT key, request_hash, transfer_id FROM idempotency_records WHERE key = $1`
	var rec repository.IdempotencyRecord
	err := exec.QueryRowContext(ctx, q, key).Scan(&rec.Key, &rec.RequestHash, &rec.TransferID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("scan idempotency: %w", err)
	}
	return &rec, nil
}

// Insert stores a new idempotency record. If the key already exists,
// it returns repository.ErrIdempotencyExists.
//
// We detect the conflict by inspecting the typed Postgres error code
// (SQLSTATE 23505 = unique_violation) rather than string-matching the
// error message. Text matching would break the moment a caller wraps
// the error, the pgx version rewords the message, or the server
// locale changes — silently returning a generic 500 instead of the
// idempotent-replay path.
//
// Note: the FK on transfer_id requires the transfer row to already
// exist in the same transaction. The service layer inserts the
// transfer first, then the idempotency record; on conflict the whole
// tx rolls back via TxManager and the service replays the winner.
func (r *IdempotencyRepo) Insert(ctx context.Context, exec repository.Executor, key, hash string, transferID uuid.UUID) error {
	const q = `INSERT INTO idempotency_records (key, request_hash, transfer_id) VALUES ($1, $2, $3)`
	_, err := exec.ExecContext(ctx, q, key, hash, transferID)
	if err == nil {
		return nil
	}
	// Map the typed Postgres error code rather than string-matching.
	// See AGENTS.md §S-7 and the comment above for why.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return repository.ErrIdempotencyExists
	}
	return fmt.Errorf("insert idempotency: %w", err)
}
