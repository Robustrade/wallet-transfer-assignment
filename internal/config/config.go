// Package config loads walletd's runtime configuration from environment
// variables, failing fast with a clear message rather than starting the
// service in a half-configured state.
package config

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
)

// Config holds every environment-derived setting walletd needs. See
// .env.example for the full list with defaults.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	LogLevel           string
	ShutdownTimeout    time.Duration
	DBMaxConns         int32
	DBStatementTimeout time.Duration
	DBLockTimeout      time.Duration
}

// Load reads and validates configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr: getString("HTTP_ADDR", ":8080"),
		LogLevel: getString("LOG_LEVEL", "info"),
	}

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	var err error
	if cfg.ShutdownTimeout, err = getDuration("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DBStatementTimeout, err = getDuration("DB_STATEMENT_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.DBLockTimeout, err = getDuration("DB_LOCK_TIMEOUT", 3*time.Second); err != nil {
		return Config{}, err
	}

	maxConns, err := getInt("DB_MAX_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	if maxConns <= 0 || maxConns > math.MaxInt32 {
		return Config{}, fmt.Errorf("DB_MAX_CONNS: must be between 1 and %d, got %d", math.MaxInt32, maxConns)
	}
	cfg.DBMaxConns = int32(maxConns)

	return cfg, nil
}

func getString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, v, err)
	}
	return d, nil
}

func getInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, v, err)
	}
	return n, nil
}
