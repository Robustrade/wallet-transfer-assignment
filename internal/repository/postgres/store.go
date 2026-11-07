// Package postgres implements the repository interfaces on PostgreSQL with pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository"
)

// Default per-transaction timeouts. A transfer holds at most two row locks for a few
// milliseconds, so waiting longer than this means something is wrong; fail fast
// and let the client retry with the same idempotency key.
const (
	DefaultLockTimeout      = 2 * time.Second
	DefaultStatementTimeout = 5 * time.Second
)

// Store is the transaction manager. Every transfer runs inside Store.WithinTx.
type Store struct {
	pool             *pgxpool.Pool
	lockTimeout      time.Duration
	statementTimeout time.Duration
}

var _ repository.TxManager = (*Store)(nil)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{
		pool:             pool,
		lockTimeout:      DefaultLockTimeout,
		statementTimeout: DefaultStatementTimeout,
	}
}

// WithTimeouts overrides the per-transaction lock and statement timeouts.
func (s *Store) WithTimeouts(lock, statement time.Duration) *Store {
	s.lockTimeout, s.statementTimeout = lock, statement
	return s
}

// WithinTx runs fn in a READ COMMITTED transaction with lock and statement timeouts.
func (s *Store) WithinTx(ctx context.Context, fn func(repo repository.Repository) error) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapError(fmt.Errorf("begin transaction: %w", err))
	}
	defer func() {
		if err != nil {
			// The error being returned matters more than a failed rollback; the
			// connection is discarded by pgx if rollback fails.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	// SET LOCAL does not accept bind parameters; the values are integers we control.
	if _, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = %d; SET LOCAL statement_timeout = %d",
		s.lockTimeout.Milliseconds(), s.statementTimeout.Milliseconds())); err != nil {
		return mapError(fmt.Errorf("set transaction timeouts: %w", err))
	}

	if err = fn(&repo{q: tx}); err != nil {
		return mapError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return mapError(fmt.Errorf("commit: %w", err))
	}
	return nil
}

// Postgres error codes that mean "rolled back, safe to retry".
var retryableCodes = map[string]bool{
	"40001": true, // serialization_failure
	"40P01": true, // deadlock_detected
	"55P03": true, // lock_not_available (lock_timeout)
	"57014": true, // query_canceled (statement_timeout)
}

func mapError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && retryableCodes[pgErr.Code] {
		return fmt.Errorf("%w: %w", domain.ErrTemporarilyUnavailable, err)
	}
	return err
}
