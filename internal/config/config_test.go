package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/config"
)

func TestAppConfig_Success(t *testing.T) {
	expectedPort := "9090"
	err := os.Setenv(config.PortKey, expectedPort)
	if err != nil {
		t.Fatal(err)
	}

	config := config.AppConfig()

	if config.Port != expectedPort {
		t.Errorf("Expected port: %s, but got: %s", expectedPort, config.Port)
	}
}

func TestAppConfig_DefaultValues(t *testing.T) {
	err := os.Unsetenv(config.PortKey)
	if err != nil {
		t.Fatal(err)
	}

	config := config.AppConfig()
	if config.Port != "8080" {
		t.Errorf("Expected default port: 8080, but got: %s", config.Port)
	}
}

func TestAppConfig_PoolAndConcurrencyDefaults(t *testing.T) {
	for _, key := range []string{
		config.MaxConcurrentRequestsKey,
		config.DBMaxOpenConnsKey,
		config.DBMaxIdleConnsKey,
		config.DBConnMaxLifetimeMinutesKey,
		config.DBConnMaxIdleTimeMinutesKey,
	} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.AppConfig()

	if cfg.MaxConcurrentRequests != 200 {
		t.Errorf("expected default MaxConcurrentRequests 200, got %d", cfg.MaxConcurrentRequests)
	}
	if cfg.DBMaxOpenConns != 25 {
		t.Errorf("expected default DBMaxOpenConns 25, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 25 {
		t.Errorf("expected default DBMaxIdleConns 25, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 5*time.Minute {
		t.Errorf("expected default DBConnMaxLifetime 5m, got %s", cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != 2*time.Minute {
		t.Errorf("expected default DBConnMaxIdleTime 2m, got %s", cfg.DBConnMaxIdleTime)
	}
}

func TestAppConfig_LogLevelDefault(t *testing.T) {
	if err := os.Unsetenv(config.LogLevelKey); err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig()
	if cfg.LogLevel != "info" {
		t.Errorf("expected default LogLevel info, got %s", cfg.LogLevel)
	}
}

func TestAppConfig_LogLevelOverride(t *testing.T) {
	t.Setenv(config.LogLevelKey, "debug")

	cfg := config.AppConfig()
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel debug, got %s", cfg.LogLevel)
	}
}

func TestAppConfig_DBURIDefaults(t *testing.T) {
	for _, key := range []string{
		"DB_USERNAME",
		"DB_PASSWORD",
		"DB_HOST",
		"DB_PORT",
		"DB_NAME",
	} {
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.AppConfig()

	expectedURI := "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
	if cfg.ReadPGURI != expectedURI {
		t.Errorf("expected default ReadPGURI %q, got %q", expectedURI, cfg.ReadPGURI)
	}
	if cfg.WritePGURI != expectedURI {
		t.Errorf("expected default WritePGURI %q, got %q", expectedURI, cfg.WritePGURI)
	}
}

func TestAppConfig_DBURIOverrides(t *testing.T) {
	t.Setenv("DB_USERNAME", "custom_user")
	t.Setenv("DB_PASSWORD", "custom_pass")
	t.Setenv("DB_HOST", "db.example.com")
	t.Setenv("DB_PORT", "6543")
	t.Setenv("DB_NAME", "custom_db")

	cfg := config.AppConfig()

	expectedURI := "postgres://custom_user:custom_pass@db.example.com:6543/custom_db?sslmode=disable"
	if cfg.ReadPGURI != expectedURI {
		t.Errorf("expected ReadPGURI %q, got %q", expectedURI, cfg.ReadPGURI)
	}
	if cfg.WritePGURI != expectedURI {
		t.Errorf("expected WritePGURI %q, got %q", expectedURI, cfg.WritePGURI)
	}
}

func TestAppConfig_PoolAndConcurrencyOverrides(t *testing.T) {
	t.Setenv(config.MaxConcurrentRequestsKey, "500")
	t.Setenv(config.DBMaxOpenConnsKey, "10")
	t.Setenv(config.DBMaxIdleConnsKey, "5")
	t.Setenv(config.DBConnMaxLifetimeMinutesKey, "15")
	t.Setenv(config.DBConnMaxIdleTimeMinutesKey, "7")

	cfg := config.AppConfig()

	if cfg.MaxConcurrentRequests != 500 {
		t.Errorf("expected MaxConcurrentRequests 500, got %d", cfg.MaxConcurrentRequests)
	}
	if cfg.DBMaxOpenConns != 10 {
		t.Errorf("expected DBMaxOpenConns 10, got %d", cfg.DBMaxOpenConns)
	}
	if cfg.DBMaxIdleConns != 5 {
		t.Errorf("expected DBMaxIdleConns 5, got %d", cfg.DBMaxIdleConns)
	}
	if cfg.DBConnMaxLifetime != 15*time.Minute {
		t.Errorf("expected DBConnMaxLifetime 15m, got %s", cfg.DBConnMaxLifetime)
	}
	if cfg.DBConnMaxIdleTime != 7*time.Minute {
		t.Errorf("expected DBConnMaxIdleTime 7m, got %s", cfg.DBConnMaxIdleTime)
	}
}
