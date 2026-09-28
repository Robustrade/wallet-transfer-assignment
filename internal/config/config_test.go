package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envFrom builds a lookup function backed by a map, so tests never touch the
// real process environment and can run in parallel.
func envFrom(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(envFrom(nil))
	require.NoError(t, err)

	assert.Equal(t, Config{
		Port:           8080,
		DBDriver:       DriverSQLite,
		DBDSN:          "file:wallet.db",
		RequestTimeout: 5 * time.Second,
		LockTimeout:    5 * time.Second,
		MaxBodyBytes:   1048576,
	}, cfg)
}

func TestLoad_EmptyValuesFallBackToDefaults(t *testing.T) {
	t.Parallel()

	// An exported-but-empty variable (e.g. "PORT=" in a .env file) should
	// behave like an unset one rather than fail parsing.
	cfg, err := Load(envFrom(map[string]string{
		"PORT":            "",
		"DB_DRIVER":       "",
		"DB_DSN":          "",
		"REQUEST_TIMEOUT": "",
		"LOCK_TIMEOUT":    "",
		"MAX_BODY_BYTES":  "",
	}))
	require.NoError(t, err)

	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, DriverSQLite, cfg.DBDriver)
	assert.Equal(t, "file:wallet.db", cfg.DBDSN)
}

func TestLoad_Overrides(t *testing.T) {
	t.Parallel()

	cfg, err := Load(envFrom(map[string]string{
		"PORT":            "9090",
		"DB_DRIVER":       "postgres",
		"DB_DSN":          "postgres://u:p@localhost:5432/wallet?sslmode=disable",
		"REQUEST_TIMEOUT": "2500ms",
		"LOCK_TIMEOUT":    "1s",
		"MAX_BODY_BYTES":  "2048",
	}))
	require.NoError(t, err)

	assert.Equal(t, Config{
		Port:           9090,
		DBDriver:       DriverPostgres,
		DBDSN:          "postgres://u:p@localhost:5432/wallet?sslmode=disable",
		RequestTimeout: 2500 * time.Millisecond,
		LockTimeout:    time.Second,
		MaxBodyBytes:   2048,
	}, cfg)
}

func TestLoad_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string // substring that must appear in the error
	}{
		{"unsupported driver", map[string]string{"DB_DRIVER": "mysql"}, "DB_DRIVER"},
		{"driver is case-sensitive", map[string]string{"DB_DRIVER": "SQLite"}, "DB_DRIVER"},
		{"postgres without dsn", map[string]string{"DB_DRIVER": "postgres"}, "DB_DSN"},
		{"port not a number", map[string]string{"PORT": "http"}, "PORT"},
		{"port zero", map[string]string{"PORT": "0"}, "PORT"},
		{"port too large", map[string]string{"PORT": "65536"}, "PORT"},
		{"request timeout unparsable", map[string]string{"REQUEST_TIMEOUT": "5"}, "REQUEST_TIMEOUT"},
		{"request timeout zero", map[string]string{"REQUEST_TIMEOUT": "0s"}, "REQUEST_TIMEOUT"},
		{"lock timeout negative", map[string]string{"LOCK_TIMEOUT": "-1s"}, "LOCK_TIMEOUT"},
		{"max body not a number", map[string]string{"MAX_BODY_BYTES": "1MB"}, "MAX_BODY_BYTES"},
		{"max body zero", map[string]string{"MAX_BODY_BYTES": "0"}, "MAX_BODY_BYTES"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(envFrom(tc.env))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	t.Parallel()

	// Operators should see every misconfiguration in one startup attempt,
	// not fix them one restart at a time.
	_, err := Load(envFrom(map[string]string{
		"DB_DRIVER": "mysql",
		"PORT":      "abc",
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DB_DRIVER")
	assert.Contains(t, err.Error(), "PORT")
}
