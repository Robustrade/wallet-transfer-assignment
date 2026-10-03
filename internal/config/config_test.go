package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, "wallet-transfer-service", cfg.ServiceName)
	assert.Equal(t, "8080", cfg.Port)
	assert.Contains(t, cfg.DatabaseURL, "wallet_transfer")
}

func TestLoad_WithJSONFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.json")

	content := `{
		"service_name": "custom-wallet",
		"port": "9090",
		"database_url": "postgres://user:pass@db.internal:5433/custom_db?sslmode=require"
	}`
	err := os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	cfg, err := Load(configPath)
	require.NoError(t, err)
	assert.Equal(t, "custom-wallet", cfg.ServiceName)
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "postgres://user:pass@db.internal:5433/custom_db?sslmode=require", cfg.DatabaseURL)
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("DATABASE_URL", "postgres://override_user@localhost:5432/override_db")

	cfg, err := Load("non_existent_file.json")
	require.NoError(t, err)
	assert.Equal(t, "3000", cfg.Port)
	assert.Equal(t, "postgres://override_user@localhost:5432/override_db", cfg.DatabaseURL)
}
