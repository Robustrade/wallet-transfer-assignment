//go:build integration

// Package testdb provides a shared PostgreSQL connection for integration
// tests.
//
// The test process expects a running Postgres instance reachable via the
// `DATABASE_URL` env var (the same variable the server reads). If the
// variable is unset, every test that calls Get / DSN / Reset is skipped
// with a clear message, so the integration suite can live in the same
// `go test ./...` invocation without forcing every contributor to have
// Docker running.
//
// Typical local workflow:
//
//	make db-up                                 # docker compose up -d postgres
//	export DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable
//	make test-int                              # or: go test -tags=integration ./...
//
// CI workflow: `.github/workflows/ci.yml` declares a `postgres:16-alpine`
// service container and sets `DATABASE_URL` on the integration step so
// the suite runs without any local setup.
//
// Migrations are applied once per process from the embedded SQL files
// in `internal/db/migrations/`. The first call also bounds the test-side
// pool so high-concurrency tests (stress sweeps, concurrent goroutine
// floods) cannot exhaust the container's `max_connections=100` default.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/db"
)

// envDatabaseURL is the env var read on first call. Kept as a named
// constant so the skip message can name the exact variable a contributor
// needs to set.
const envDatabaseURL = "DATABASE_URL"

// Shared state. `once` guards startup so the first test pays the
// migrate cost and every later test gets the cached handle.
var (
	once      sync.Once
	sharedDB  *sql.DB
	sharedDSN string
	sharedErr error
)

// Get returns a *sql.DB pointed at a freshly migrated database. If
// DATABASE_URL is unset, the test is skipped via tb.Skip — that's
// always the right call in integration tests because there's nothing
// useful to assert without a database.
//
// Takes a testing.TB so it can be called from both *testing.T (tests)
// and *testing.B (benchmarks).
func Get(tb testing.TB) *sql.DB {
	tb.Helper()
	once.Do(func() {
		sharedDB, sharedErr = start()
	})
	if sharedErr != nil {
		// Distinguish "skip because no DSN configured" from
		// "fail because the configured DSN didn't work".
		if sharedErr == errNoDSN {
			tb.Skipf("integration tests require %s (e.g. `make db-up && export %s=postgres://...`); skipping",
				envDatabaseURL, envDatabaseURL)
		}
		tb.Fatalf("open postgres at %s: %v", envDatabaseURL, sharedErr)
	}
	return sharedDB
}

// DSN returns the configured connection string. Useful for tests that
// need to spawn their own *sql.DB (for example, cmd/server tests that
// go through config.Load).
func DSN(tb testing.TB) string {
	tb.Helper()
	_ = Get(tb)
	return sharedDSN
}

// Reset truncates every table. Call from the start of any test (or
// benchmark) that mutates data so it does not see leftovers from
// prior runs.
//
// RESTART IDENTITY CASCADE resets the BIGSERIAL counter on
// ledger_entries — without it, ledger ids would creep upward across
// tests and any test that asserts on specific ids would be flaky.
// CASCADE follows FKs so we don't have to worry about ordering.
func Reset(tb testing.TB) {
	tb.Helper()
	d := Get(tb)
	_, err := d.ExecContext(context.Background(),
		`TRUNCATE idempotency_records, ledger_entries, transfers, wallets RESTART IDENTITY CASCADE`)
	if err != nil {
		tb.Fatalf("reset db: %v", err)
	}
}

// errNoDSN is the sentinel returned by start() when DATABASE_URL is
// unset. Get() checks for it to distinguish "skip" from "fail".
var errNoDSN = fmt.Errorf("%s is not set", envDatabaseURL)

// start opens the configured database, applies migrations, and bounds
// the pool. Internal — only called once, via sync.Once, from Get.
func start() (*sql.DB, error) {
	dsn := os.Getenv(envDatabaseURL)
	if dsn == "" {
		return nil, errNoDSN
	}
	sharedDSN = dsn

	// Bound the test-side pool so high-concurrency tests cannot
	// exhaust the container's max_connections=100. Production has
	// its own bounds via cmd/server; mirror them here so test
	// behaviour matches production behaviour under load.
	d, err := db.OpenWithPool(dsn, db.PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 1 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	if err := db.Migrate(d); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}
