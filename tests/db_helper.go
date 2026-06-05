package tests

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupDB(t *testing.T) *pgxpool.Pool {

	t.Helper()

	connStr :=
		"postgres://postgres:postgres@localhost:5432/walletdb"

	pool, err :=
		pgxpool.New(
			context.Background(),
			connStr,
		)

	if err != nil {
		t.Fatal(err)
	}

	cleanDB(t, pool)

	return pool
}

func cleanDB(
	t *testing.T,
	pool *pgxpool.Pool,
) {

	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`
		TRUNCATE
			ledger_entries,
			transfers,
			wallets
		RESTART IDENTITY
		CASCADE
		`,
	)

	if err != nil {
		t.Fatal(err)
	}
}
