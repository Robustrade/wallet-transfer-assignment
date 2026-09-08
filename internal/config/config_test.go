package config_test

import (
	"testing"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DBMaxConns != 10 {
		t.Fatalf("DBMaxConns = %d, want 10", cfg.DBMaxConns)
	}
	if !cfg.AutoMigrate {
		t.Fatal("AutoMigrate should default to true")
	}
}

func TestLoad_RequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error when DATABASE_URL is unset")
	}
}

func TestLoad_RejectsBadInt(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("DB_MAX_OPEN_CONNS", "not-a-number")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected an error for a non-integer DB_MAX_OPEN_CONNS")
	}
}
