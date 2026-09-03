package service_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// testDB opens a connection to a real Postgres instance (see
// docker-compose.yml) and resets the schema before each test so tests are
// independent of each other and of execution order. Tests in this package
// are integration tests by design: the whole point of the locking and
// idempotency strategy is how it behaves against a real database, which a
// mock cannot exercise meaningfully.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Skipf("skipping: no reachable postgres at %s (run `make up` first): %v", dsn, err)
	}

	resetSchema(t, db)
	return db
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`
		TRUNCATE TABLE idempotency_records, ledger_entries, transfers, wallets RESTART IDENTITY CASCADE
	`)
	if err != nil {
		t.Fatalf("reset schema: %v", err)
	}

	seed := readFile(t, repoPath("seed", "dev_seed.sql"))
	if _, err := db.Exec(seed); err != nil {
		t.Fatalf("apply seed: %v", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// repoPath resolves a path relative to the repository root regardless of
// which package's test binary is running.
func repoPath(parts ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(append([]string{root}, parts...)...)
}
