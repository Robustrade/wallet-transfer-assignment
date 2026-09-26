// Package testutil wires integration tests to a real PostgreSQL database.
//
// Tests never mock the database: the properties under test (row locking,
// unique constraints, transaction rollback) only exist in PostgreSQL, so the
// suite runs against the same engine as production.
package testutil

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet/internal/store/postgres"
	"wallet/migrations"
)

// defaultTestDatabaseURL points at the docker-compose database container,
// using a separate database so tests never touch development data.
const defaultTestDatabaseURL = "postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable"

// lockTimeoutMS matches the application default (config.LockTimeoutMS).
const lockTimeoutMS = 2000

// DatabaseURL returns the test database URL: TEST_DATABASE_URL if set,
// otherwise a database next to the development one.
func DatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultTestDatabaseURL
}

// redactURL removes the password from a PostgreSQL connection URL so it is
// safe to include in logs and error messages. If the URL cannot be parsed the
// original string is returned unchanged.
func redactURL(raw string) string {
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		// Cannot parse the URL; return a safe placeholder rather than
		// echoing the raw value, which may contain an embedded password.
		return "<redacted: malformed URL>"
	}
	// Rebuild as host:port/database — enough context to diagnose a
	// connection failure without leaking credentials.
	return fmt.Sprintf("%s:%d/%s", cfg.Host, cfg.Port, cfg.Database)
}

// Run prepares the test database and runs the suite. Every test package calls
// it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }
func Run(m *testing.M) int {
	ctx := context.Background()

	url := DatabaseURL()
	if err := ensureDatabase(ctx, url); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %v\n", err)
		return 1
	}

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testutil: connect %s: %v\n", redactURL(url), err)
		return 1
	}
	defer conn.Close(context.WithoutCancel(ctx))

	if _, err := migrations.Apply(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: migrate: %v\n", err)
		return 1
	}

	// All test packages share one database, and each NewEnv truncates every
	// table; packages run concurrently by default, so without this lock two
	// suites would wipe each other's fixtures. A session advisory lock
	// serializes whole suites while tests inside a package run as usual.
	if err := lockSuite(ctx, conn); err != nil {
		fmt.Fprintf(os.Stderr, "testutil: %v\n", err)
		return 1
	}
	return m.Run()
}

// suiteLockKey is an arbitrary, stable key for the "tests are using this
// database" advisory lock. The wait is bounded so a stuck suite fails loudly
// instead of hanging forever.
const (
	suiteLockKey            = 0x7465737473756974 // "testsuit"
	databaseCreationLockKey = 0x7465737464626372 // "testdbcr"
	suiteWait               = 10 * time.Minute
)

func lockSuite(ctx context.Context, conn *pgx.Conn) error {
	waitCtx, cancel := context.WithTimeout(ctx, suiteWait)
	defer cancel()

	if _, err := conn.Exec(waitCtx, "SELECT pg_advisory_lock($1)", suiteLockKey); err != nil {
		return fmt.Errorf("waiting for another test suite (held > %s): %w", suiteWait, err)
	}
	return nil
}

// ensureDatabase creates the target database if it does not exist yet.
func ensureDatabase(ctx context.Context, url string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("parse %s: %w", redactURL(url), err)
	}
	target := cfg.Database
	if target == "" {
		return fmt.Errorf("database name missing in %s", redactURL(url))
	}

	admin := cfg.Copy()
	admin.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, admin)
	if err != nil {
		return fmt.Errorf("connect to maintenance database: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	// Test packages begin concurrently. Serialize the check/create sequence on
	// the maintenance database so two packages cannot both observe a missing
	// target and race to CREATE DATABASE.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", databaseCreationLockKey); err != nil {
		return fmt.Errorf("lock test database creation: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", databaseCreationLockKey)
	}()

	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", target).Scan(&exists); err != nil {
		return fmt.Errorf("check database %s: %w", target, err)
	}
	if exists {
		return nil
	}
	// CREATE DATABASE cannot run inside a transaction and takes no bind
	// parameters; the identifier is quoted by pgx.
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{target}.Sanitize()); err != nil {
		var existsAfterRace bool
		if checkErr := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", target).Scan(&existsAfterRace); checkErr == nil && existsAfterRace {
			return nil
		}
		return fmt.Errorf("create database %s: %w", target, err)
	}
	return nil
}

// Env is a test environment: a pool plus a Store bound to the test database.
type Env struct {
	Pool  *pgxpool.Pool
	Store *postgres.Store
}

// NewEnv resets the database and returns a connected environment, using the
// application's default lock_timeout. The pool is closed automatically when
// the test finishes.
func NewEnv(t *testing.T) *Env {
	t.Helper()
	return newEnv(t, lockTimeoutMS)
}

// NewEnvWithLockTimeout is NewEnv with an explicit lock_timeout, so tests that
// exercise lock contention can observe the timeout in test time instead of
// waiting out the production default.
func NewEnvWithLockTimeout(t *testing.T, timeoutMS int) *Env {
	t.Helper()
	return newEnv(t, timeoutMS)
}

func newEnv(t *testing.T, timeoutMS int) *Env {
	t.Helper()

	ctx := context.Background()
	url := DatabaseURL()

	store, err := postgres.New(ctx, url, 16, timeoutMS)
	if err != nil {
		t.Fatalf("testutil: connect store: %v", err)
	}
	t.Cleanup(store.Close)

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("testutil: connect pool: %v", err)
	}
	t.Cleanup(pool.Close)

	env := &Env{Pool: pool, Store: store}
	env.Reset(t)
	return env
}

// Reset truncates every table and restarts identity sequences, so each test
// starts from an empty database.
func (e *Env) Reset(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := e.Pool.Exec(ctx,
		"TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("testutil: reset database: %v", err)
	}
}

// CreateWallet inserts a wallet with the given balance and returns its id.
func (e *Env) CreateWallet(t *testing.T, id string, balance int64) string {
	t.Helper()
	ctx := context.Background()

	if _, err := e.Pool.Exec(ctx,
		"INSERT INTO wallets (id, balance) VALUES ($1, $2)", id, balance); err != nil {
		t.Fatalf("testutil: create wallet %s: %v", id, err)
	}
	return id
}

// Balance reads a wallet's balance directly from the database.
func (e *Env) Balance(t *testing.T, walletID string) int64 {
	t.Helper()
	var balance int64
	if err := e.Pool.QueryRow(context.Background(),
		"SELECT balance FROM wallets WHERE id = $1", walletID).Scan(&balance); err != nil {
		t.Fatalf("testutil: read balance of %s: %v", walletID, err)
	}
	return balance
}

// CountRows counts rows matching a query (e.g. "SELECT count(*) FROM ledger_entries").
func (e *Env) CountRows(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := e.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("testutil: count rows (%s): %v", query, err)
	}
	return n
}
