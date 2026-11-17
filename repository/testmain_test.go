package repository_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/config"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	config.LoadDotEnv()
	// Prefer TEST_DATABASE_URL; never fall back to DATABASE_URL so TRUNCATE
	// cannot wipe the hand-used app database.
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5433/wallet_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		failOrSkipDB("cannot connect: " + err.Error())
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		failOrSkipDB("ping failed: " + err.Error())
	}

	_, file, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(file), "..", "migrations")
	if err := repository.Migrate(ctx, pool, migrationsDir); err != nil {
		pool.Close()
		os.Stderr.WriteString("migrate failed: " + err.Error() + "\n")
		os.Exit(1)
	}

	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// failOrSkipDB exits the package on DB setup failure unless SKIP_DB_TESTS=1
// (explicit opt-in). Returning success on every connect error would make
// go test report PASS without running any repository tests.
func failOrSkipDB(msg string) {
	if os.Getenv("SKIP_DB_TESTS") == "1" {
		os.Stderr.WriteString("skipping postgres tests (SKIP_DB_TESTS=1): " + msg + "\n")
		os.Exit(0)
	}
	os.Stderr.WriteString("postgres test setup failed: " + msg + "\n")
	os.Stderr.WriteString("start Postgres (docker compose up -d) or set SKIP_DB_TESTS=1 to skip\n")
	os.Exit(1)
}

func truncateAll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := testPool.Exec(ctx, `
		TRUNCATE ledger_entries, transfers, idempotency_records, wallets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func requirePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testPool == nil {
		t.Skip("no test database")
	}
	return testPool
}
