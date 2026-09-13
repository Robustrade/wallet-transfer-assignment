package datasource

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		want  int
		valid bool
	}{
		{name: "valid", file: "001_init.sql", want: 1, valid: true},
		{name: "missing separator", file: "001", valid: false},
		{name: "invalid number", file: "abc_init.sql", valid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := migrationVersion(test.file)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("migration version: got %d, err %v", got, err)
				}
			} else if err == nil {
				t.Fatal("expected invalid migration filename error")
			}
		})
	}
}

func TestRunMigrationsAppliesAndSkipsMigration(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("db", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("db", "migrations", "001_test.sql"), []byte("CREATE TABLE migrated (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	con, err := sql.Open("sqlite", "file:migrations-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()

	if err := runMigrations(con, testLogger{}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	if err := runMigrations(con, testLogger{}); err != nil {
		t.Fatalf("rerun migrations: %v", err)
	}

	var count int
	if err := con.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration count: got %d, want 1", count)
	}
}

func TestRunMigrationsRejectsInvalidMigrationFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("db", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("db", "migrations", "invalid.sql"), []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}

	con, err := sql.Open("sqlite", "file:invalid-migration-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()

	if err := runMigrations(con, testLogger{}); err == nil {
		t.Fatal("expected invalid migration filename error")
	}
}

func TestMigrationHelpersHandleClosedDatabase(t *testing.T) {
	con, err := sql.Open("sqlite", "file:closed-migration-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if err := con.Close(); err != nil {
		t.Fatal(err)
	}

	if err := ensureMigrationTable(con); err == nil {
		t.Fatal("expected ensure migration table error")
	}
	if _, err := isMigrationApplied(con, 1); err == nil {
		t.Fatal("expected migration status error")
	}
}

func TestApplyMigration(t *testing.T) {
	con, err := sql.Open("sqlite", "file:apply-migration-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()
	if err := ensureMigrationTable(con); err != nil {
		t.Fatal(err)
	}

	if err := applyMigration(con, 1, "CREATE TABLE applied (id INTEGER PRIMARY KEY);"); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if err := applyMigration(con, 1, "CREATE TABLE duplicate (id INTEGER PRIMARY KEY);"); err == nil {
		t.Fatal("expected duplicate migration version error")
	}
	if err := applyMigration(con, 2, "INVALID SQL"); err == nil || !strings.Contains(err.Error(), "SQL") {
		t.Fatalf("expected SQL migration error, got %v", err)
	}
}

func TestNewDataSource(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Chdir(t.TempDir())
		dataSource, err := NewDataSource(testLogger{})
		if err != nil || dataSource == nil {
			t.Fatalf("new datasource: got %v, err %v", dataSource, err)
		}
	})

	t.Run("database open failure", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.Mkdir("wallet.db", 0o755); err != nil {
			t.Fatal(err)
		}
		dataSource, err := NewDataSource(testLogger{})
		if err == nil || dataSource != nil {
			t.Fatalf("expected datasource initialization error, got %v, %v", dataSource, err)
		}
	})

	t.Run("migration failure is logged and datasource is returned", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.MkdirAll(filepath.Join("db", "migrations"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("db", "migrations", "001_bad.sql"), []byte("INVALID SQL"), 0o644); err != nil {
			t.Fatal(err)
		}
		dataSource, err := NewDataSource(testLogger{})
		if err != nil || dataSource == nil {
			t.Fatalf("expected datasource despite migration error: %v, %v", dataSource, err)
		}
	})
}
