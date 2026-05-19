package config_test

import (
	"testing"

	"github.com/hariharandr/wallet-transfer-assignment/internal/config"
)

func TestLoad_RequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty, got nil")
	}
}

func TestLoad_DefaultsHTTPAddr(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.DatabaseURL != "postgres://x" {
		t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://x")
	}
}
