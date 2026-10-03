package tests

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

func resetDatabase(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		"TRUNCATE ledger_entries, transfers, wallets RESTART IDENTITY CASCADE",
	)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}

func seedWallets(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`INSERT INTO wallets (id, balance)
		 VALUES ('wallet_1', 1000), ('wallet_2', 500)`,
	)
	if err != nil {
		t.Fatalf("seed wallets: %v", err)
	}
}

func queryWalletBalance(
	t *testing.T,
	pool *pgxpool.Pool,
	walletID string,
) int64 {
	t.Helper()

	var balance int64

	err := pool.QueryRow(
		context.Background(),
		"SELECT balance FROM wallets WHERE id = $1",
		walletID,
	).Scan(&balance)

	if err != nil {
		t.Fatalf("query wallet %s: %v", walletID, err)
	}

	return balance
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string) int {
	t.Helper()

	var count int

	err := pool.QueryRow(
		context.Background(),
		query,
	).Scan(&count)

	if err != nil {
		t.Fatalf("count rows: %v", err)
	}

	return count
}

func requireBalance(
	t *testing.T,
	pool *pgxpool.Pool,
	walletID string,
	expected int64,
) {
	t.Helper()

	actual := queryWalletBalance(t, pool, walletID)

	if actual != expected {
		t.Fatalf(
			"wallet %s balance = %d, want %d",
			walletID,
			actual,
			expected,
		)
	}
}

func requireErrorContains(t *testing.T, err error, expected string) {
	t.Helper()

	if err == nil {
		t.Fatalf("expected error containing %q, got nil", expected)
	}

	if !strings.Contains(err.Error(), expected) {
		t.Fatalf(
			"error = %q, want it to contain %q",
			err.Error(),
			expected,
		)
	}
}
