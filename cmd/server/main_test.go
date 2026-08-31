package main

import (
	"context"
	"net/http"
	"os"
	"testing"
)

// TestDatabaseURLUsesEnvironmentAndDefault verifies server database configuration selection.
func TestDatabaseURLUsesEnvironmentAndDefault(t *testing.T) {
	old := os.Getenv("DATABASE_URL")
	t.Cleanup(func() { _ = os.Setenv("DATABASE_URL", old) })
	_ = os.Unsetenv("DATABASE_URL")
	if got := databaseURL(); got == "" {
		t.Fatal("expected local database default")
	}
	if err := os.Setenv("DATABASE_URL", "postgres://custom"); err != nil {
		t.Fatal(err)
	}
	if got := databaseURL(); got != "postgres://custom" {
		t.Fatalf("url=%s", got)
	}
}

// TestRunBuildsTheServerWithoutBindingAPort verifies dependency wiring at startup.
func TestRunBuildsTheServerWithoutBindingAPort(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to execute PostgreSQL server integration tests")
	}
	original := serveHTTP
	serveHTTP = func(address string, handler http.Handler) error {
		if address != ":8080" || handler == nil {
			t.Fatal("server was not configured")
		}
		return nil
	}
	t.Cleanup(func() { serveHTTP = original })
	if err := run(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
}

// TestRunReturnsDatabaseConnectionErrors verifies startup failures are surfaced to main.
func TestRunReturnsDatabaseConnectionErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, "postgres://wallet:wallet@localhost:5433/wallets?sslmode=disable"); err == nil {
		t.Fatal("expected connection error")
	}
}

// TestMainUsesTheConfiguredDatabaseAndServer verifies the executable entrypoint returns after a clean server exit.
func TestMainUsesTheConfiguredDatabaseAndServer(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to execute PostgreSQL server integration tests")
	}
	oldDSN := os.Getenv("DATABASE_URL")
	original := serveHTTP
	t.Cleanup(func() { _ = os.Setenv("DATABASE_URL", oldDSN); serveHTTP = original })
	if err := os.Setenv("DATABASE_URL", dsn); err != nil {
		t.Fatal(err)
	}
	serveHTTP = func(string, http.Handler) error { return nil }
	main()
}
