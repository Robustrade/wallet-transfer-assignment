package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer/pkg/logger"
)

// DBTX is satisfied by both *pgxpool.Pool and pgx.Tx.
// Repositories accept this interface so the service layer can pass
// either a pool (for reads) or a transaction (for writes).
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var pool *pgxpool.Pool

// Connect initialises the connection pool.
func Connect(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}

	cfg.MaxConns = 25
	cfg.MinConns = 5
	cfg.MaxConnIdleTime = 30 * time.Second

	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}

	if err := p.Ping(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	pool = p
	logger.Info("database.Connect", "connected to PostgreSQL", nil)
	return nil
}

// GetPool returns the global connection pool.
func GetPool() *pgxpool.Pool {
	return pool
}

// Close shuts down the connection pool.
func Close() {
	if pool != nil {
		pool.Close()
	}
}

// BeginTx starts a serializable transaction and returns it.
func BeginTx(ctx context.Context) (pgx.Tx, error) {
	return pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
}

// IsUniqueViolation returns true when the error is a PostgreSQL unique constraint violation (code 23505).
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
