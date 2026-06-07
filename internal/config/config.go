// Package config loads runtime configuration from environment variables with
// sensible defaults, keeping all environment parsing in one place.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime configuration for the server.
type Config struct {
	// HTTPAddr is the listen address for the API server.
	HTTPAddr string
	// DatabaseURL is the PostgreSQL DSN. When empty the server runs with the
	// in-memory store, which is handy for local exploration and demos.
	DatabaseURL string
	// DBMaxConns bounds the connection pool size.
	DBMaxConns int32
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
	// ReadTimeout and WriteTimeout protect the HTTP server from slow clients.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getEnv(EnvHTTPAddr, DefaultHTTPAddr),
		DatabaseURL:     getEnv(EnvDatabaseURL, ""),
		ShutdownTimeout: 15 * time.Second,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    10 * time.Second,
	}

	maxConns, err := getEnvInt(EnvDBMaxConns, 10)
	if err != nil {
		return Config{}, err
	}
	cfg.DBMaxConns = int32(maxConns)

	return cfg, nil
}

// UsesInMemoryStore reports whether the server should run with the in-memory
// store (no DATABASE_URL configured).
func (c Config) UsesInMemoryStore() bool {
	return c.DatabaseURL == ""
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid integer for %s: %w", key, err)
	}
	return n, nil
}
