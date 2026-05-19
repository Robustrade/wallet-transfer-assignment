package config

import "testing"

func TestDatabase_DSN(t *testing.T) {
	db := Database{
		Host:     "localhost",
		Port:     "5432",
		User:     "wallet",
		Password: "secret",
		Name:     "wallet_transfer",
		SSLMode:  "disable",
	}

	got := db.DSN()
	want := "postgres://wallet:secret@localhost:5432/wallet_transfer?sslmode=disable"
	if got != want {
		t.Fatalf("DSN() = %q, want %q", got, want)
	}
}

func TestLoadDatabase_fromEnvVars(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("POSTGRES_HOST", "db.example.com")
	t.Setenv("WALLET_DB_HOST_PORT", "5433")
	t.Setenv("POSTGRES_USER", "app")
	t.Setenv("POSTGRES_PASSWORD", "pw")
	t.Setenv("POSTGRES_DB", "wallets")
	t.Setenv("POSTGRES_SSLMODE", "require")

	db := LoadDatabase()
	if db.Host != "db.example.com" || db.Port != "5433" || db.User != "app" ||
		db.Password != "pw" || db.Name != "wallets" || db.SSLMode != "require" {
		t.Fatalf("unexpected database config: %+v", db)
	}
}

func TestLoadDatabase_prefersDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://urluser:urlpass@urlhost:5434/url_db?sslmode=verify-full")
	t.Setenv("POSTGRES_HOST", "ignored")

	db := LoadDatabase()
	if db.Host != "urlhost" || db.Port != "5434" || db.User != "urluser" ||
		db.Password != "urlpass" || db.Name != "url_db" || db.SSLMode != "verify-full" {
		t.Fatalf("unexpected database config: %+v", db)
	}
}
