package config

import (
	"testing"
	"time"
)

func TestLoad_DefaultsAndValidation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DB_POOL_MAX_CONNS", "")
	t.Setenv("DB_POOL_MIN_CONNS", "")
	t.Setenv("HTTP_REQUEST_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr=%q", cfg.HTTPAddr)
	}
	if cfg.DBPoolMaxConns != 20 || cfg.DBPoolMinConns != 2 {
		t.Fatalf("pool max=%d min=%d", cfg.DBPoolMaxConns, cfg.DBPoolMinConns)
	}
	if cfg.HTTPRequestTimeout != 60*time.Second {
		t.Fatalf("request timeout=%s", cfg.HTTPRequestTimeout)
	}
}

func TestLoad_RequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_RejectsInvalidPoolBounds(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("DB_POOL_MAX_CONNS", "2")
	t.Setenv("DB_POOL_MIN_CONNS", "5")
	if _, err := Load(); err == nil {
		t.Fatal("expected min > max error")
	}
}
