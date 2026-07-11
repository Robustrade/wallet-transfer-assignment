//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/migrate"
)

var testPool *pgxpool.Pool

// testPostgresDSN points at the postgres brought up by `docker-compose up
// -d postgres redis` (see `make test-integration`), overridable for CI.
func testPostgresDSN() string {
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	dsn := testPostgresDSN()

	if err := migrate.Run(dsn, "../../../migrations"); err != nil {
		fmt.Fprintln(os.Stderr, "run migrations:", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create pool:", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ping postgres — is `docker-compose up -d postgres` running?", err)
		os.Exit(1)
	}

	testPool = pool
	os.Exit(m.Run())
}
