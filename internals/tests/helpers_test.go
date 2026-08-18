package service

import (
	"os"
	"testing"
)

// requireTestDSN reads DATABASE_URL and skips the test with a clear message
// if it's not set, rather than failing with a confusing connection error.
// Point this at a real (ideally disposable/local) Postgres instance with the
// schema already applied — these are integration tests, not unit tests, and
// intentionally hit a real database to prove real locking behavior.
func requireTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return dsn
}
