package config

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseURLBuildsFromParts(t *testing.T) {
	got, err := DatabaseURL("", "db.example", "5433", "wallet user", "p@ss:/word", "wallets", "require")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "db.example:5433" || parsed.User.Username() != "wallet user" || parsed.Path != "/wallets" || parsed.Query().Get("sslmode") != "require" {
		t.Fatalf("unexpected database URL: %s", got)
	}
	password, ok := parsed.User.Password()
	if !ok || password != "p@ss:/word" {
		t.Fatalf("password was not safely encoded")
	}
}

func TestDatabaseURLRequiresUserAndName(t *testing.T) {
	if _, err := DatabaseURL("", "", "", "", "", "", ""); err != nil {
		t.Fatalf("empty configuration should allow caller to decide whether DB is required: %v", err)
	}
	if _, err := DatabaseURL("", "localhost", "5432", "wallet", "", "", ""); err == nil {
		t.Fatal("expected missing database name error")
	}
}

func TestLoadEnvFileDoesNotOverrideExistingEnvironment(t *testing.T) {
	const key = "ROBUSTRADE_CONFIG_TEST"
	const databaseKey = "ROBUSTRADE_CONFIG_DB_NAME_TEST"
	t.Setenv(key, "from-shell")
	previous, wasSet := os.LookupEnv(databaseKey)
	if err := os.Unsetenv(databaseKey); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if wasSet {
			_ = os.Setenv(databaseKey, previous)
		} else {
			_ = os.Unsetenv(databaseKey)
		}
	})
	path := filepath.Join(t.TempDir(), "test-local.env")
	if err := os.WriteFile(path, []byte(key+"=from-file\n"+databaseKey+"='wallet db'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(key); got != "from-shell" {
		t.Fatalf("existing environment value overwritten: %q", got)
	}
	if got := os.Getenv(databaseKey); got != "wallet db" {
		t.Fatalf("quoted value = %q, want wallet db", got)
	}
}
