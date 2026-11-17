package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds process settings loaded from the environment.
type Config struct {
	DatabaseURL    string
	HTTPAddr       string
	MigrationsDir  string
	DBPoolMaxConns int32
	DBPoolMinConns int32
	// HTTP server timeouts (bound accept/read/write/idle).
	HTTPReadHeaderTimeout time.Duration
	HTTPReadTimeout       time.Duration
	HTTPWriteTimeout      time.Duration
	HTTPIdleTimeout       time.Duration
	// Per-request context deadline (cancels pool acquire + DB work).
	HTTPRequestTimeout time.Duration
}

// Load reads configuration after LoadDotEnv. DATABASE_URL is required.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		HTTPAddr:              envOr("HTTP_ADDR", ":8080"),
		MigrationsDir:         envOr("MIGRATIONS_DIR", "migrations"),
		DBPoolMaxConns:        int32(envIntOr("DB_POOL_MAX_CONNS", 20)),
		DBPoolMinConns:        int32(envIntOr("DB_POOL_MIN_CONNS", 2)),
		HTTPReadHeaderTimeout: envDurationOr("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		HTTPReadTimeout:       envDurationOr("HTTP_READ_TIMEOUT", 10*time.Second),
		HTTPWriteTimeout:      envDurationOr("HTTP_WRITE_TIMEOUT", 65*time.Second),
		HTTPIdleTimeout:       envDurationOr("HTTP_IDLE_TIMEOUT", 60*time.Second),
		HTTPRequestTimeout:    envDurationOr("HTTP_REQUEST_TIMEOUT", 60*time.Second),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required (set in .env or the environment)")
	}
	if cfg.DBPoolMaxConns < 1 {
		return Config{}, fmt.Errorf("DB_POOL_MAX_CONNS must be >= 1")
	}
	if cfg.DBPoolMinConns < 0 || cfg.DBPoolMinConns > cfg.DBPoolMaxConns {
		return Config{}, fmt.Errorf("DB_POOL_MIN_CONNS must be between 0 and DB_POOL_MAX_CONNS")
	}
	if cfg.HTTPRequestTimeout <= 0 {
		return Config{}, fmt.Errorf("HTTP_REQUEST_TIMEOUT must be > 0")
	}
	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
