//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/testutil"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dsn := testutil.PostgresDSN()

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
