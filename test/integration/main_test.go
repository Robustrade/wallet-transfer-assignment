// Package integration_test drives the real HTTP stack (postgres
// repositories, service layer, httpapi router) over a live Postgres
// database. Every test in this package requires TEST_DATABASE_URL — see
// docs/design.md §2.1 — and none of them call t.Parallel(), since they
// share one database and internal/postgres's own tests also touch it (the
// Makefile pins `go test` to -p 1 for exactly this reason).
package integration_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/postgres"
)

var testDB *postgres.DB

func TestMain(m *testing.M) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		db, err := postgres.NewDB(ctx, postgres.Config{
			DatabaseURL:      url,
			MaxConns:         20,
			StatementTimeout: 5 * time.Second,
			LockTimeout:      3 * time.Second,
		})
		if err != nil {
			cancel()
			fmt.Fprintln(os.Stderr, "connect to test database:", err)
			os.Exit(1)
		}
		if err := postgres.Migrate(ctx, db.Pool()); err != nil {
			cancel()
			fmt.Fprintln(os.Stderr, "migrate test database:", err)
			os.Exit(1)
		}
		cancel()
		testDB = db
	}

	code := m.Run()

	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

// requireDB skips the calling test if no test database is configured, with
// the same message internal/postgres's schema tests use.
func requireDB(t *testing.T) *postgres.DB {
	t.Helper()
	if testDB == nil {
		t.Skip("set TEST_DATABASE_URL to run integration tests (try: make test)")
	}
	return testDB
}
