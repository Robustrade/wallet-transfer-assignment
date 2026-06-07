// Package postgres implements the repository ports on top of PostgreSQL using
// the pgx driver and connection pool.
//
// Concurrency strategy: every transfer runs inside a single SERIALIZABLE-safe
// transaction that takes pessimistic row locks (SELECT ... FOR UPDATE) on the
// participating wallets. Locks are taken in a deterministic order by the
// service layer to avoid deadlocks. Idempotency is enforced by a UNIQUE
// constraint on transfers.idempotency_key combined with INSERT ... ON CONFLICT.
package postgres

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

// Store is a PostgreSQL-backed repository.Store.
type Store struct {
	pool *pgxpool.Pool
}

// Config holds the tunables for the connection pool. Sensible defaults are
// applied for any zero value.
type Config struct {
	DSN             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func (c *Config) applyDefaults() {
	if c.MaxConns == 0 {
		c.MaxConns = 10
	}
	if c.MinConns == 0 {
		c.MinConns = 2
	}
	if c.MaxConnLifetime == 0 {
		c.MaxConnLifetime = time.Hour
	}
	if c.MaxConnIdleTime == 0 {
		c.MaxConnIdleTime = 30 * time.Minute
	}
}

// New connects to PostgreSQL, configures the pool and verifies connectivity.
func New(ctx context.Context, cfg Config) (*Store, error) {
	cfg.applyDefaults()

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Migrate applies the embedded schema. It is idempotent (CREATE TABLE IF NOT
// EXISTS) so it is safe to run on every startup for this assignment.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Close releases the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// WithinTx runs fn inside a database transaction, committing on success and
// rolling back on any error or panic.
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context, repos repository.Repositories) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	repos := repository.Repositories{
		Wallets:   &walletRepo{q: tx},
		Transfers: &transferRepo{q: tx},
		Ledger:    &ledgerRepo{q: tx},
	}
	if err = fn(ctx, repos); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// Reader returns repositories bound to the pool for non-transactional reads.
func (s *Store) Reader() repository.Repositories {
	return repository.Repositories{
		Wallets:   &walletRepo{q: s.pool},
		Transfers: &transferRepo{q: s.pool},
		Ledger:    &ledgerRepo{q: s.pool},
	}
}

// querier is the subset of pgx used by the repositories. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so the same repository code serves transactional and
// non-transactional access.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
