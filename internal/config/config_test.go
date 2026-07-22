package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/config"
)

func TestConfig_Defaults(t *testing.T) {
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, int32(20), cfg.MaxDBConns)
	require.Equal(t, 5, cfg.MaxAttempts)
	require.Equal(t, 4, cfg.WorkerCount)
	require.False(t, cfg.TracingEnabled)
	require.Contains(t, cfg.DatabaseURL, "localhost:5432")
	require.Contains(t, cfg.DatabaseURL, "mydb")
}

func TestConfig_EnvOverride(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://custom/db")
	t.Setenv("WORKER_COUNT", "8")
	t.Setenv("TRACING_ENABLED", "true")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":9999", cfg.HTTPAddr)
	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, "postgres://custom/db", cfg.DatabaseURL)
	require.Equal(t, 8, cfg.WorkerCount)
	require.True(t, cfg.TracingEnabled)
}

func TestConfig_AssemblesDatabaseURLFromParts(t *testing.T) {
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("DB_PORT", "6543")
	t.Setenv("DB_USER", "u")
	t.Setenv("DB_PASSWORD", "p")
	t.Setenv("DB_NAME", "wallets")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "postgres://u:p@db.internal:6543/wallets?sslmode=disable", cfg.DatabaseURL)
}
