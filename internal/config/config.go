// Package config loads service configuration from the environment, applying
// sensible defaults so the service is runnable with zero configuration in
// local and containerised setups.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all runtime configuration for the wallet service.
type Config struct {
	// HTTPAddr is the listen address for the HTTP server, e.g. ":8080".
	HTTPAddr string
	// DatabaseURL is the PostgreSQL connection string (pgx/libpq format).
	DatabaseURL string
	// ReadTimeout bounds reading the full request including the body.
	ReadTimeout time.Duration
	// WriteTimeout bounds writing the response.
	WriteTimeout time.Duration
	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment.
//
// Recognised variables:
//
//	HTTP_ADDR         (default ":8080")
//	DATABASE_URL      (required; e.g. postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable)
//	READ_TIMEOUT      (default "10s")
//	WRITE_TIMEOUT     (default "10s")
//	SHUTDOWN_TIMEOUT  (default "15s")
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ReadTimeout:     getEnvDuration("READ_TIMEOUT", 10*time.Second),
		WriteTimeout:    getEnvDuration("WRITE_TIMEOUT", 10*time.Second),
		ShutdownTimeout: getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
