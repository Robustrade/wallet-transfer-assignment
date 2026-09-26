// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration.
type Config struct {
	// HTTPAddr is the address the API server listens on.
	HTTPAddr string
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// LockTimeoutMS bounds how long any single database lock acquisition
	// (row locks, speculative-insert waits) may take before the transaction
	// is aborted. See README for why this exists.
	LockTimeoutMS int
	// RunMigrations makes the API server apply embedded migrations at startup.
	// Off by default: migrations are an explicit operator action (cmd/migrate),
	// so a rolling deploy never has two instances racing schema changes.
	RunMigrations bool
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:      getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://wallet:wallet@localhost:5433/wallet?sslmode=disable"),
		LockTimeoutMS: 2000,
	}

	if raw := os.Getenv("RUN_MIGRATIONS"); raw != "" {
		// strconv.ParseBool accepts 1/t/T/TRUE/true/... and rejects typos;
		// a misspelled value must not silently disable migrations.
		run, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid RUN_MIGRATIONS %q: must be a boolean", raw)
		}
		cfg.RunMigrations = run
	}
	if raw := os.Getenv("LOCK_TIMEOUT_MS"); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil || ms <= 0 {
			return Config{}, fmt.Errorf("invalid LOCK_TIMEOUT_MS %q: must be a positive integer", raw)
		}
		cfg.LockTimeoutMS = ms
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL must not be empty")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
