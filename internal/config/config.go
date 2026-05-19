// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration, populated from environment
// variables so the same binary runs unchanged across local, CI
type Config struct {
	DatabaseURL string // libpq/pgx connection str
	HTTPAddr    string // HTTP listen addr
}

// Load reads configuration from the environment, applying sensible defaults
// for local development. DATABASE_URL is required.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    getenvDefault("HTTP_ADDR", ":8080"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
