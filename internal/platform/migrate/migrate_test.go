package migrate_test

import (
	"context"
	"testing"

	"github.com/hariharandr/wallet-transfer-assignment/testsupport"
)

func TestMigrations_CreateSchemaAndSeed(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	ctx := context.Background()

	// all 4 tables from the plan should exist after migrate.
	want := []string{"wallets", "transfers", "ledger_entries", "idempotency_records"}
	for _, tbl := range want {
		var exists bool
		err := pool.QueryRow(ctx,
			`select exists (select 1 from information_schema.tables
			 where table_schema='public' and table_name=$1)`, tbl).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", tbl, err)
		}
		if !exists {
			t.Fatalf("table %q missing after migration", tbl)
		}
	}

	// seed wallets should be there so manual + later tests have something to move.
	var n int
	if err := pool.QueryRow(ctx, `select count(*) from wallets`).Scan(&n); err != nil {
		t.Fatalf("count wallets: %v", err)
	}
	if n < 3 {
		t.Fatalf("expected >=3 seed wallets, got %d", n)
	}
}
