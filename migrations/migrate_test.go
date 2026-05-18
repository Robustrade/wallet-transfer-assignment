package migrations

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestApplySQLite(t *testing.T) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("pragma: %v", err)
	}

	if err := Apply(db, "sqlite"); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	// Second run should skip already-applied migrations.
	if err := Apply(db, "sqlite"); err != nil {
		t.Fatalf("re-apply migrations: %v", err)
	}

	var migrationCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected 1 applied migration, got %d", migrationCount)
	}

	tables := []string{"wallets", "transfers", "ledger_entries", "idempotency_records"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
}
