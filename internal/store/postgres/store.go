// Package postgres implements the service storage ports on PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/service"
)

// boundedPoolCtx bounds one pool-backed call that runs outside InTx: the
// acquire and the statement share the same 5 s budget. A saturated pool is
// then shed as a retry-safe 503 (an acquire timeout maps to
// ErrDatabaseUnavailable) instead of queueing without bound; reads run without
// a server-side statement_timeout, so this client-side budget is also their
// only cap.
func boundedPoolCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, constants.ConnectTimeout)
}

// Store is the PostgreSQL-backed implementation of service.Store.
type Store struct {
	pool          *pgxpool.Pool
	lockTimeoutMS int
}

var _ service.Store = (*Store)(nil)

// New connects a pool to databaseURL. lockTimeoutMS bounds every lock
// acquisition inside transfer transactions (see README, "Concurrency").
func New(ctx context.Context, databaseURL string, maxConns int32, lockTimeoutMS int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = maxConns
	// Fail fast instead of hanging: the API should not sit on an unreachable
	// database while holding client connections.
	cfg.ConnConfig.ConnectTimeout = constants.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool, lockTimeoutMS: lockTimeoutMS}, nil
}

// Close releases the connection pool.
func (s *Store) Close() { s.pool.Close() }

// Ping verifies database connectivity (used by the health endpoint).
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := boundedPoolCtx(ctx)
	defer cancel()
	return s.pool.Ping(ctx)
}

// InTx runs fn inside a single READ COMMITTED transaction.
//
// READ COMMITTED is required, not merely chosen: it is the isolation level in
// which a waiting INSERT ... ON CONFLICT observes the just-committed
// conflicting row (under REPEATABLE READ the statement would fail with 40001
// instead, turning idempotent retries into sporadic errors).
//
// lock_timeout and statement_timeout are applied as the transaction's first
// statement (one combined SET LOCAL) so that neither a lock wait nor a single
// statement can stall indefinitely and hold the connection.
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Tx) error) error {
	// Bounding the pool acquire keeps a saturated pool from turning into
	// unbounded request latency: better to shed load with a 503 than to queue
	// every request behind an exhausted pool.
	acquireCtx, cancel := context.WithTimeout(ctx, constants.ConnectTimeout)
	tx, err := s.pool.BeginTx(acquireCtx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	cancel()
	if err != nil {
		return mapError(fmt.Errorf("begin transaction: %w", err))
	}
	// Rollback is a no-op after a successful commit, so this defer covers
	// every early return. WithoutCancel keeps cleanup working even when the
	// request context is already cancelled.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Both budgets are set in a single round trip: pgx sends argument-less
	// Exec over the simple protocol, which allows multiple statements per
	// call. SET LOCAL is transaction-scoped, so the budgets disappear at
	// commit or rollback. The values are ints; string formatting is safe.
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		constants.SetLocalTransactionTimeoutsSQL,
		s.lockTimeoutMS, constants.StatementTimeoutMS)); err != nil {
		return mapError(fmt.Errorf("set transaction timeouts: %w", err))
	}

	if err := fn(ctx, &txStore{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// The COMMIT may or may not have reached the server: the outcome is
		// unknown and must be resolved before reporting anything to the
		// client. Both the cause and the sentinel are kept for errors.Is/As.
		return fmt.Errorf("%w: %w", domain.ErrCommitOutcomeUnknown, err)
	}
	return nil
}

// txStore adapts a pgx.Tx to service.Tx.
type txStore struct {
	tx pgx.Tx
}

var _ service.Tx = (*txStore)(nil)

// querier is the read surface shared by *pgxpool.Pool and pgx.Tx, letting the
// read helpers serve both in-transaction and pool-backed callers.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// mapError translates PostgreSQL error codes and transport failures into
// domain sentinels. Only conditions that tell the caller "nothing happened,
// retry" are classified:
//
//   - 55P03 (lock_timeout) -> ErrLockTimeout (503)
//   - 57014 (statement_timeout) -> ErrStatementTimeout (503)
//   - anything that is not a PostgreSQL server error (dial failures, broken
//     connections, pool-acquire timeouts) -> ErrDatabaseUnavailable (503)
//
// Real SQL-level errors (constraint violations, syntax errors, ...) are
// server-side bugs, not capacity problems: they are returned unchanged and
// become 500. ErrNoRows never reaches mapError (handled at each call site);
// a cancelled request context is the client's doing and is kept intact.
//
// Call-site contract: mapError is fed exclusively by errors returned from
// pgx calls (Exec/Query/QueryRow/rows.Err/BeginTx). Invariant violations
// this package detects itself -- the rowcount assertions guarding the
// terminal transfer transitions, CreditWallet, InsertLedgerEntries and
// CompleteIdempotency -- are constructed as plain errors and returned
// directly, never passed through mapError. They are bugs, not capacity
// problems, so they must keep turning into 500 rather than 503.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "55P03": // lock_not_available: lock_timeout or NOWAIT expired
			return fmt.Errorf("%w: %s", domain.ErrLockTimeout, pgErr.Message)
		case "57014": // query_canceled: statement_timeout expired
			return fmt.Errorf("%w: %s", domain.ErrStatementTimeout, pgErr.Message)
		}
		return err
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %w", domain.ErrDatabaseUnavailable, err)
}
