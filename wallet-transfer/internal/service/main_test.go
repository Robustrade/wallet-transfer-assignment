package service_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// pool is shared across all tests in this package. Each test truncates first.
var pool *pgxpool.Pool

// TestMain spins up one Postgres for the whole package (fast), applies the
// schema, and tears it down. Set WALLET_TEST_DATABASE_URL to point at an
// existing Postgres and skip the container entirely.
func TestMain(m *testing.M) {
	ctx := context.Background()

	var dsn string
	var cleanup func()

	if url := os.Getenv("WALLET_TEST_DATABASE_URL"); url != "" {
		dsn = url
		cleanup = func() {}
	} else {
		container, connStr, err := startPostgres(ctx)
		if err != nil {
			log.Fatalf("start postgres: %v", err)
		}
		dsn = connStr
		cleanup = func() { _ = container.Terminate(ctx) }
	}

	var err error
	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		cleanup()
		log.Fatalf("connect pool: %v", err)
	}
	if err := pingWithRetry(ctx, pool, 30, time.Second); err != nil {
		pool.Close()
		cleanup()
		log.Fatalf("ping: %v", err)
	}
	if err := migrate.Apply(ctx, pool); err != nil {
		pool.Close()
		cleanup()
		log.Fatalf("apply schema: %v", err)
	}

	code := m.Run()

	pool.Close()
	cleanup()
	os.Exit(code)
}

// startPostgres launches a postgres:16-alpine container and returns its DSN.
func startPostgres(ctx context.Context) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:16-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "test",
			"POSTGRES_PASSWORD": "test",
			"POSTGRES_DB":       "wallet",
		},
		// Postgres logs this line twice: once during init, once when truly ready.
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}
	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", err
	}
	port, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return nil, "", err
	}
	dsn := fmt.Sprintf("postgres://test:test@%s:%s/wallet?sslmode=disable", host, port.Port())
	return container, dsn, nil
}

func pingWithRetry(ctx context.Context, p *pgxpool.Pool, attempts int, gap time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = p.Ping(ctx); err == nil {
			return nil
		}
		time.Sleep(gap)
	}
	return err
}

// --- helpers ---

func reset(t *testing.T) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func seedWallet(t *testing.T, id string, bal domain.Amount) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, bal)
	if err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}

func balance(t *testing.T, id string) domain.Amount {
	t.Helper()
	var b domain.Amount
	if err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("read balance %s: %v", id, err)
	}
	return b
}

func derivedBalance(t *testing.T, id string) domain.Amount {
	t.Helper()
	var b domain.Amount
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(CASE WHEN type='CREDIT' THEN amount ELSE -amount END),0)
		   FROM ledger_entries WHERE wallet_id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("derived balance %s: %v", id, err)
	}
	return b
}

func countLedgerEntries(t *testing.T) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_entries`).Scan(&n); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return n
}

func countTransfersByStatus(t *testing.T, status string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM transfers WHERE status = $1`, status).Scan(&n); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	return n
}

func key(prefix string, i int) string { return fmt.Sprintf("%s-%d", prefix, i) }
