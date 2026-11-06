package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/config"
)

func TestLoad_AppliesDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")

	cfg, err := config.Load()

	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, slog.LevelInfo, cfg.LogLevel)
	require.Equal(t, int32(20), cfg.DBMaxConns)
	require.Equal(t, 3*time.Second, cfg.DBLockTimeout)
	require.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
}

func TestLoad_ReportsEveryInvalidSetting(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("LOG_LEVEL", "LOUD")
	t.Setenv("DB_MAX_CONNS", "0")
	t.Setenv("DB_LOCK_TIMEOUT", "soon")

	_, err := config.Load()

	require.Error(t, err)
	for _, want := range []string{"DATABASE_URL", "LOG_LEVEL", "DB_MAX_CONNS", "DB_LOCK_TIMEOUT"} {
		require.ErrorContains(t, err, want)
	}
}

func TestHealthcheckURL(t *testing.T) {
	t.Setenv("HEALTHCHECK_URL", "")
	t.Setenv("HTTP_ADDR", ":9090")
	require.Equal(t, "http://127.0.0.1:9090/readyz", config.HealthcheckURL())

	t.Setenv("HEALTHCHECK_URL", "http://app:8080/readyz")
	require.Equal(t, "http://app:8080/readyz", config.HealthcheckURL())
}
