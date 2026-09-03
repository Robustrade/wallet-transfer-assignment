package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
)

func Run(t *testing.T, fn func(t *testing.T, db *database.DB)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		fn(t, open(t, "sqlite://"+filepath.Join(t.TempDir(), "wallet.db")))
	})

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Log("TEST_DATABASE_URL not set; skipping PostgreSQL backend")
		return
	}
	t.Run("postgres", func(t *testing.T) {
		fn(t, open(t, postgresSchemaDSN(t, dsn)))
	})
}

func open(t *testing.T, dsn string) *database.DB {
	t.Helper()

	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func postgresSchemaDSN(t *testing.T, dsn string) string {
	t.Helper()

	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = admin.Close() }()

	schema := "test_" + sanitize(t.Name())
	if _, err := admin.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema)); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	if _, err := admin.Exec(fmt.Sprintf("CREATE SCHEMA %q", schema)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		cleanup, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer func() { _ = cleanup.Close() }()
		_, _ = cleanup.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func sanitize(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, name)
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}
