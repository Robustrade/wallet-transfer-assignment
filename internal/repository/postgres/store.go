// Package postgres implements the repository contracts on PostgreSQL via pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository"
)

// PostgreSQL error codes that mean "retry the whole transaction".
const (
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
	codeUniqueViolation      = "23505"
)

// dbtx is the subset of pgx shared by *pgxpool.Pool and pgx.Tx, so the same
// repository code runs both inside and outside a transaction.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the PostgreSQL implementation of repository.Store.
type Store struct {
	pool       *pgxpool.Pool
	maxRetries int
	backoff    time.Duration
	repos
}

var _ repository.Store = (*Store)(nil)

// NewStore wraps a connection pool. Transactions that fail with a deadlock or
// serialization error are retried up to three times.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, maxRetries: 3, backoff: 10 * time.Millisecond, repos: newRepos(pool)}
}

// WithinTx implements repository.Store.
//
// Isolation is READ COMMITTED: correctness comes from explicit row locks
// (SELECT ... FOR UPDATE) and database constraints rather than from a stronger
// isolation level, which avoids blanket serialization-failure retries.
func (s *Store) WithinTx(ctx context.Context, fn func(repository.Repositories) error) error {
	for attempt := 0; ; attempt++ {
		err := s.runTx(ctx, fn)
		if err == nil || !isRetryable(err) || attempt >= s.maxRetries {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.backoff << attempt):
		}
	}
}

func (s *Store) runTx(ctx context.Context, fn func(repository.Repositories) error) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		// Rollback after a successful Commit is a harmless no-op.
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(newRepos(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func isRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == codeSerializationFailure || pgErr.Code == codeDeadlockDetected
}

func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// repos binds every repository to one dbtx (the pool or a transaction).
type repos struct {
	wallets     *walletRepo
	transfers   *transferRepo
	ledger      *ledgerRepo
	idempotency *idempotencyRepo
}

func newRepos(db dbtx) repos {
	return repos{
		wallets:     &walletRepo{db: db},
		transfers:   &transferRepo{db: db},
		ledger:      &ledgerRepo{db: db},
		idempotency: &idempotencyRepo{db: db},
	}
}

func (r repos) Wallets() repository.WalletRepository          { return r.wallets }
func (r repos) Transfers() repository.TransferRepository      { return r.transfers }
func (r repos) Ledger() repository.LedgerRepository           { return r.ledger }
func (r repos) Idempotency() repository.IdempotencyRepository { return r.idempotency }
