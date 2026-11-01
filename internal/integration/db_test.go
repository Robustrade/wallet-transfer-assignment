package integration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/migrations"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		os.Exit(m.Run())
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(url)
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

func requireDB(t *testing.T) *postgres.Store {
	t.Helper()
	if testPool == nil {
		t.Skip("DATABASE_URL is not set")
	}
	if _, err := testPool.Exec(context.Background(), `
		TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE
	`); err != nil {
		t.Fatal(err)
	}
	return postgres.New(testPool)
}
