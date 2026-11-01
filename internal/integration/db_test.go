package integration

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/migrations"
)

var testPool *pgxpool.Pool

// dbName matches the database names this suite is willing to create.
var dbName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func TestMain(m *testing.M) {
	serviceURL := os.Getenv("DATABASE_URL")
	if serviceURL == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	testURL, err := ensureTestDatabase(ctx, serviceURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare test database: %v\n", err)
		os.Exit(1)
	}
	cfg, err := pgxpool.ParseConfig(testURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse database url: %v\n", err)
		os.Exit(1)
	}
	cfg.MaxConns = 40
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "database ping: %v\n", err)
		os.Exit(1)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// ensureTestDatabase returns a URL for a database named "{service}_test".
// Truncation in the suite stays inside that database, so make run data is left intact.
func ensureTestDatabase(ctx context.Context, serviceURL string) (string, error) {
	name, err := databaseName(serviceURL)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(name, "_test") {
		return serviceURL, nil
	}
	if !dbName.MatchString(name) {
		return "", fmt.Errorf("database name %q cannot be used to derive a test database", name)
	}
	testName := name + "_test"
	adminURL, err := withDatabase(serviceURL, "postgres")
	if err != nil {
		return "", err
	}
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		return "", err
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		admin, err = pgxpool.New(ctx, serviceURL)
		if err != nil {
			return "", err
		}
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, testName).Scan(&exists); err != nil {
		return "", fmt.Errorf("check test database: %w", err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE `+quoteIdent(testName)); err != nil {
			return "", fmt.Errorf("create test database: %w", err)
		}
	}
	return withDatabase(serviceURL, testName)
}

func TestMigrationsWaitForConcurrentStartup(t *testing.T) {
	requireDB(t)
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wallet-transfer-assignment:migrations')::bigint)`); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- migrations.Apply(ctx, testPool)
	}()

	select {
	case err := <-done:
		t.Fatalf("migration completed while the lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("migration did not continue after the lock was released")
	}
}

func databaseName(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	if name == "" {
		return "", fmt.Errorf("database url has no database name")
	}
	return name, nil
}

func withDatabase(rawURL, name string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	parsed.Path = "/" + name
	return parsed.String(), nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func TestTestDatabaseURLKeepsServiceDatabase(t *testing.T) {
	t.Parallel()
	const serviceURL = "postgres://wallet:wallet@127.0.0.1:5432/wallet?sslmode=disable"
	name, err := databaseName(serviceURL)
	if err != nil {
		t.Fatal(err)
	}
	if name != "wallet" {
		t.Fatalf("database name = %s", name)
	}
	testURL, err := withDatabase(serviceURL, name+"_test")
	if err != nil {
		t.Fatal(err)
	}
	if testURL == serviceURL {
		t.Fatal("test url matches the service url")
	}
	got, err := databaseName(testURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "wallet_test" {
		t.Fatalf("test database = %s", got)
	}
}

func requireDB(t *testing.T) *postgres.Store {
	t.Helper()
	if testPool == nil {
		t.Skip("DATABASE_URL is not set")
	}
	// This pool is the dedicated test database. The service database is not truncated.
	if _, err := testPool.Exec(context.Background(), `
		TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatal(err)
	}
	return postgres.New(testPool)
}
