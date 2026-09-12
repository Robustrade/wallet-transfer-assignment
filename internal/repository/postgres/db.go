package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/migrations"
)

// DBTX is the common interface implemented by *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const (
	// DefaultMaxConns defines default maximum connection pool limit.
	DefaultMaxConns int32 = 30
	// DefaultMinConns defines default minimum idle connection pool limit.
	DefaultMinConns int32 = 5
	// DefaultHeartbeatHeadroom defines reserved pool connections for background heartbeats and non-tx queries.
	DefaultHeartbeatHeadroom int32 = 10
)

// NewPool initializes and tests a PostgreSQL connection pool.
func NewPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}

	// Preserve user-configured pool_max_conns from connString if specified; otherwise use DefaultMaxConns
	if !strings.Contains(connString, "pool_max_conns") {
		config.MaxConns = DefaultMaxConns
	}
	if !strings.Contains(connString, "pool_min_conns") {
		config.MinConns = DefaultMinConns
	}
	if config.MinConns > config.MaxConns {
		config.MinConns = config.MaxConns
	}
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgxpool: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	return pool, nil
}

// NewHeartbeatPool creates an isolated connection pool specifically for background idempotency lease heartbeats,
// guaranteeing that burst transfer transactions can never starve heartbeat lease renewals.
func NewHeartbeatPool(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres config for heartbeat pool: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create heartbeat pgxpool: %w", err)
	}
	return pool, nil
}

// Migrate applies the database migrations from the migrations package.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrations.Migrate(ctx, pool)
}
