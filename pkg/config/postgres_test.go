package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPostgresConfig_Success(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(wd)
	}()

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change directory: %v", err)
	}

	envFile := filepath.Join(tmpDir, ".env")
	envContents := "POSTGRES_USER=testuser\nPOSTGRES_PASSWORD=testpass\nPOSTGRES_DB=testdb\nPOSTGRES_HOST=localhost\nPOSTGRES_PORT=5432\n"
	if err := os.WriteFile(envFile, []byte(envContents), 0o600); err != nil {
		t.Fatalf("failed to write .env file: %v", err)
	}

	cfg, err := LoadPostgresConfig()
	if err != nil {
		t.Fatalf("LoadPostgresConfig returned error: %v", err)
	}

	if cfg.User != "testuser" {
		t.Fatalf("expected User=testuser, got %q", cfg.User)
	}
	if cfg.Password != "testpass" {
		t.Fatalf("expected Password=testpass, got %q", cfg.Password)
	}
	if cfg.DB != "testdb" {
		t.Fatalf("expected DB=testdb, got %q", cfg.DB)
	}
	if cfg.Host != "localhost" {
		t.Fatalf("expected Host=localhost, got %q", cfg.Host)
	}
	if cfg.Port != "5432" {
		t.Fatalf("expected Port=5432, got %q", cfg.Port)
	}
}
