// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	DBMaxConns      int32
	DBConnTimeout   time.Duration
	LogLevel        string
	ShutdownTimeout time.Duration
	// AutoMigrate applies embedded migrations on startup when true.
	AutoMigrate bool
}

// Load reads configuration from environment variables, applying defaults. It
// returns an error only for malformed values, not missing ones.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		LogLevel:        getenv("LOG_LEVEL", "info"),
		ShutdownTimeout: 10 * time.Second,
	}

	maxConns, err := getenvInt("DB_MAX_OPEN_CONNS", 10)
	if err != nil {
		return Config{}, err
	}
	cfg.DBMaxConns = int32(maxConns)

	timeoutSecs, err := getenvInt("DB_CONNECT_TIMEOUT_SECONDS", 5)
	if err != nil {
		return Config{}, err
	}
	cfg.DBConnTimeout = time.Duration(timeoutSecs) * time.Second

	cfg.AutoMigrate = getenvBool("DB_AUTO_MIGRATE", true)

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate checks that required values are present. DATABASE_URL is required
// unless the in-memory repository is selected (REPO=memory).
func (c Config) validate() error {
	if c.DatabaseURL == "" && !strings.EqualFold(os.Getenv("REPO"), "memory") {
		return fmt.Errorf("config: DATABASE_URL is required (or set REPO=memory)")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be an integer: %w", key, err)
	}
	return n, nil
}

func getenvBool(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return b
}
