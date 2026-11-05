package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func setValidDatabaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://wallet:secret@localhost:5432/wallets?sslmode=disable")
	t.Setenv("DB_MAX_OPEN_CONNS", "")
	t.Setenv("DB_MAX_IDLE_CONNS", "")
	t.Setenv("DB_CONN_MAX_LIFETIME", "")
}

func TestLoadConfigDefaults(t *testing.T) {
	setValidDatabaseEnv(t)
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got.MaxOpenConns != defaultMaxOpenConns || got.MaxIdleConns != defaultMaxIdleConns ||
		got.ConnMaxLifetime != defaultConnMaxLifetime {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if got.DatabaseURL == "" {
		t.Fatal("DatabaseURL should be set")
	}
}

func TestLoadConfigCustomPoolSettings(t *testing.T) {
	setValidDatabaseEnv(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "40")
	t.Setenv("DB_MAX_IDLE_CONNS", "10")
	t.Setenv("DB_CONN_MAX_LIFETIME", "2m")
	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if got.MaxOpenConns != 40 || got.MaxIdleConns != 10 || got.ConnMaxLifetime != 2*time.Minute {
		t.Fatalf("unexpected custom config: %+v", got)
	}
}

func TestLoadConfigRejectsInvalidSettings(t *testing.T) {
	tests := []struct {
		name, url, maxOpen, maxIdle, lifetime string
	}{
		{name: "missing URL"},
		{name: "unsupported scheme", url: "mysql://localhost/wallets"},
		{name: "missing host", url: "postgres:///wallets"},
		{name: "missing database", url: "postgres://localhost"},
		{name: "invalid max open", url: "postgres://localhost/wallets", maxOpen: "zero"},
		{name: "non-positive max idle", url: "postgres://localhost/wallets", maxIdle: "0"},
		{name: "idle exceeds open", url: "postgres://localhost/wallets", maxOpen: "2", maxIdle: "3"},
		{name: "invalid lifetime", url: "postgres://localhost/wallets", lifetime: "soon"},
		{name: "non-positive lifetime", url: "postgres://localhost/wallets", lifetime: "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tt.url)
			t.Setenv("DB_MAX_OPEN_CONNS", tt.maxOpen)
			t.Setenv("DB_MAX_IDLE_CONNS", tt.maxIdle)
			t.Setenv("DB_CONN_MAX_LIFETIME", tt.lifetime)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("LoadConfig() error = nil, want error")
			}
		})
	}
}


func TestLoadConfigMalformedURLDoesNotExposeCredentials(t *testing.T) {
	const databaseURL = "postgres://wallet:secret%ZZ@localhost:5432/wallets"
	t.Setenv("DATABASE_URL", databaseURL)

	_, err := LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want malformed URL error")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), databaseURL) {
		t.Fatalf("LoadConfig() error exposes database credentials: %v", err)
	}
}

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	if _, err := Open(context.Background(), "pgx", Config{}); err == nil {
		t.Fatal("Open() error = nil, want invalid configuration error")
	}
}

func TestOpenRejectsUnregisteredDriver(t *testing.T) {
	cfg := Config{
		DatabaseURL:     "postgres://localhost/wallets",
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
	}
	if _, err := Open(context.Background(), "driver-that-is-not-registered", cfg); err == nil {
		t.Fatal("Open() error = nil, want unknown driver error")
	}
}
