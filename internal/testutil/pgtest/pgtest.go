// Package pgtest gives integration tests their own throwaway Postgres database.
//
// Every call creates a uniquely named database on the server at TEST_DATABASE_URL
// and drops it when the test finishes, so tests are fully isolated and may run in
// parallel. Without TEST_DATABASE_URL the calling test is skipped.
package pgtest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Robustrade/wallet-transfer-assignment/migrations"
)

// EnvVar names the connection string of a Postgres server where tests may create databases.
const EnvVar = "TEST_DATABASE_URL"

// NewEmptyDB returns a pool connected to a fresh, empty database.
func NewEmptyDB(t testing.TB) *pgxpool.Pool {
	t.Helper()

	adminURL := os.Getenv(EnvVar)
	if adminURL == "" {
		t.Skipf("%s not set; skipping integration test (run `make test` to run it in Docker)", EnvVar)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	name := "test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	execAdmin(ctx, t, adminURL, "CREATE DATABASE "+name)

	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatalf("parse %s: %v", EnvVar, err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 32

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		execAdmin(cleanupCtx, t, adminURL, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool
}

// NewDB returns a pool connected to a fresh database with all migrations applied.
func NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool := NewEmptyDB(t)
	if _, err := migrate.Up(context.Background(), pool, migrations.FS, Logger()); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return pool
}

// Logger returns a logger that discards output, keeping test logs readable.
func Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func execAdmin(ctx context.Context, t testing.TB, adminURL, sql string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to %s: %v", EnvVar, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
