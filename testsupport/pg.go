// test-only helpers for spinning a real postgres in docker.
// kept out of the main module graph so prod never imports testcontainers.
package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/hariharandr/wallet-transfer-assignment/internal/platform/migrate"
)

// StartPostgres boots a disposable postgres, applies all migrations and
// returns a connected pool. container is torn down on test cleanup.
// skips when -short (no docker in that mode / CI fast path).
func StartPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping docker-backed test in -short mode")
	}

	ctx := context.Background()

	pg, err := tcpg.Run(ctx, "postgres:16-alpine",
		tcpg.WithDatabase("wallet"),
		tcpg.WithUsername("wallet"),
		tcpg.WithPassword("wallet"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	if err := migrate.Run(dsn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}
