// Package pgtest provides a migrated PostgreSQL database for integration tests.
//
// By default it starts a disposable postgres:16 container (Docker required). Set
// TEST_DATABASE_URL to use an existing database instead, e.g. on CI runners
// without Docker. Run `go test -short` to skip database tests entirely.
package pgtest

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/celinagupta/wallet-transfer-assignment/migrations"
)

var pool *pgxpool.Pool

// Main is called from a package's TestMain. It provides the database, runs the
// package's tests and cleans up.
func Main(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run()) // database tests call Pool, which skips them
	}

	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pgtest:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	ctx := context.Background()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("wallet_test"),
			tcpostgres.WithUsername("wallet"),
			tcpostgres.WithPassword("wallet"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		)
		if err != nil {
			return 0, fmt.Errorf("start postgres container (is Docker running? or set TEST_DATABASE_URL): %w", err)
		}
		defer func() { _ = testcontainers.TerminateContainer(container) }()

		if url, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			return 0, fmt.Errorf("container connection string: %w", err)
		}
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return 0, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 32 // enough for the concurrency tests to actually contend
	if pool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
		return 0, fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := migrations.Up(ctx, stdlib.OpenDBFromPool(pool)); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// Pool returns a pool on a freshly emptied database. Tests that use it must not
// call t.Parallel, since they share one database.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("database test skipped in -short mode")
	}
	if pool == nil {
		t.Fatal("pgtest.Main was not called from TestMain")
	}
	if _, err := pool.Exec(context.Background(), `TRUNCATE ledger_entries, transfers, wallets`); err != nil {
		t.Fatalf("reset database: %v", err)
	}
	return pool
}

// SeedWallets inserts wallets with the given opening balances.
func SeedWallets(t testing.TB, p *pgxpool.Pool, balances map[string]int64) {
	t.Helper()
	for id, balance := range balances {
		if _, err := p.Exec(context.Background(),
			`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance); err != nil {
			t.Fatalf("seed wallet %s: %v", id, err)
		}
	}
}

// Balance returns a wallet's current balance.
func Balance(t testing.TB, p *pgxpool.Pool, walletID string) int64 {
	t.Helper()
	var b int64
	if err := p.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&b); err != nil {
		t.Fatalf("balance of %s: %v", walletID, err)
	}
	return b
}

// Count returns the number of rows in table (one of the fixed table names).
func Count(t testing.TB, p *pgxpool.Pool, table string) int {
	t.Helper()
	switch table {
	case "wallets", "transfers", "ledger_entries":
	default:
		t.Fatalf("unknown table %q", table)
	}
	var n int
	if err := p.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
