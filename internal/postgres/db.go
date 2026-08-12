package postgres

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// dbtx is satisfied by both *pgxpool.Pool and pgx.Tx, so repository
// implementations can run against either a bare pool (non-transactional
// reads) or an in-flight transaction (writes) without duplicating code.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Config controls pool sizing and timeout behaviour.
type Config struct {
	DatabaseURL      string
	MaxConns         int32
	StatementTimeout time.Duration
	LockTimeout      time.Duration
}

// DB wraps a pgxpool.Pool and implements service.UnitOfWork.
type DB struct {
	pool        *pgxpool.Pool
	lockTimeout time.Duration
}

// NewDB opens a connection pool and configures a per-connection
// statement_timeout so no single query can hang the pool indefinitely.
func NewDB(ctx context.Context, cfg Config) (*DB, error) {
	pgCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns > 0 {
		pgCfg.MaxConns = cfg.MaxConns
	}
	stmtTimeoutMS := cfg.StatementTimeout.Milliseconds()
	pgCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = %d", stmtTimeoutMS))
		return err
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return &DB{pool: pool, lockTimeout: cfg.LockTimeout}, nil
}

// Pool exposes the underlying pool for callers that need it directly (the
// migrator, health checks).
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Close releases all pooled connections.
func (db *DB) Close() { db.pool.Close() }

// Ping verifies the database is reachable.
func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// Wallets, Transfers and Ledger implement service.Repos for
// non-transactional reads (e.g. the idempotency replay fast path), backed
// directly by the pool.
func (db *DB) Wallets() service.WalletRepository     { return &walletRepository{q: db.pool} }
func (db *DB) Transfers() service.TransferRepository { return &transferRepository{q: db.pool} }
func (db *DB) Ledger() service.LedgerRepository      { return &ledgerRepository{q: db.pool} }

const (
	maxTxAttempts  = 3
	retryBaseDelay = 10 * time.Millisecond
)

// Do implements service.UnitOfWork. It runs fn inside a transaction,
// retrying the whole transaction with jittered backoff on serialization
// failures (40001) and deadlocks (40P01) — see docs/design.md §7. Retrying
// is safe here because fn's only side effects are the repository calls it
// makes through the Repos it is given, and the whole operation is
// idempotent by construction (guarded by the idempotency key and the
// guarded balance updates).
func (db *DB) Do(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) error {
	var lastErr error
	for attempt := 1; attempt <= maxTxAttempts; attempt++ {
		err := db.doOnce(ctx, fn)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryableCode(err) || attempt == maxTxAttempts {
			return err
		}
		backoff := time.Duration(attempt)*retryBaseDelay + time.Duration(rand.Intn(int(retryBaseDelay.Milliseconds())))*time.Millisecond
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return lastErr
}

func (db *DB) doOnce(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) (err error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()

	if db.lockTimeout > 0 {
		if _, lockErr := tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", db.lockTimeout.Milliseconds())); lockErr != nil {
			return fmt.Errorf("set lock_timeout: %w", lockErr)
		}
	}

	repos := &txRepos{
		wallets:   &walletRepository{q: tx},
		transfers: &transferRepository{q: tx},
		ledger:    &ledgerRepository{q: tx},
	}

	return fn(ctx, repos)
}

type txRepos struct {
	wallets   service.WalletRepository
	transfers service.TransferRepository
	ledger    service.LedgerRepository
}

func (r *txRepos) Wallets() service.WalletRepository     { return r.wallets }
func (r *txRepos) Transfers() service.TransferRepository { return r.transfers }
func (r *txRepos) Ledger() service.LedgerRepository      { return r.ledger }
