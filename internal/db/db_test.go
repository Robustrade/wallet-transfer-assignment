package db_test

import (
	"context"
	"testing"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/db"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/dbtest"
	"github.com/sagarsuperuser/wallet-transfer-assignment/migrations"
)

func TestMigrateIsRepeatable(t *testing.T) {
	pool := dbtest.Pool(t)
	ctx := context.Background()

	// dbtest.Pool already migrated; further calls must be no-ops.
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
			t.Fatalf("re-running migrations: %v", err)
		}
	}

	var applied int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations WHERE version = '0001_init.sql'`,
	).Scan(&applied); err != nil {
		t.Fatalf("count applied migrations: %v", err)
	}

	if applied != 1 {
		t.Fatalf("migration recorded %d times, want exactly 1", applied)
	}
}

func TestMigrateCreatesEveryTable(t *testing.T) {
	pool := dbtest.Pool(t)

	for _, table := range []string{"wallets", "transfers", "ledger_entries"} {
		var exists bool
		if err := pool.QueryRow(context.Background(),
			`SELECT to_regclass($1) IS NOT NULL`, table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s was not created", table)
		}
	}
}
