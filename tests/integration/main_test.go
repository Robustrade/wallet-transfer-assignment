// Package integration runs behavioral tests against a real database.
//
// Every test runs on SQLite. When TEST_POSTGRES_DSN is set, it also runs on
// PostgreSQL, in its own freshly created schema.
package integration

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for pgAdmin
)

// pgDSN is the Postgres DSN from the environment, or "" to skip Postgres.
var pgDSN = os.Getenv("TEST_POSTGRES_DSN")

// pgAdmin is a connection used only to create and drop per-test schemas.
var pgAdmin *sql.DB

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if pgDSN != "" {
		db, err := sql.Open("pgx", pgDSN)
		if err == nil {
			err = db.Ping()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "TEST_POSTGRES_DSN is set but Postgres is unreachable: %v\n", err)
			return 1
		}
		pgAdmin = db
		defer db.Close()
	}
	return m.Run()
}
