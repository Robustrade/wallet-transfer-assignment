// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the service configuration. See .env.example for documentation.
type Config struct {
	DatabaseURL     string
	HTTPAddr        string
	LogLevel        slog.Level
	DBMaxConns      int32
	DBLockTimeout   time.Duration
	DBConnectWait   time.Duration
	ShutdownTimeout time.Duration
}

// Load reads the configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(getenv("LOG_LEVEL", "INFO"))); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}
	maxConns, err := strconv.ParseInt(getenv("DB_MAX_CONNS", "20"), 10, 32)
	if err != nil || maxConns < 1 {
		errs = append(errs, errors.New("DB_MAX_CONNS must be a positive integer"))
	}
	cfg.DBMaxConns = int32(maxConns)
	cfg.DBLockTimeout = duration("DB_LOCK_TIMEOUT", "3s", &errs)
	cfg.DBConnectWait = duration("DB_CONNECT_WAIT", "30s", &errs)
	cfg.ShutdownTimeout = duration("SHUTDOWN_TIMEOUT", "10s", &errs)
	return cfg, errors.Join(errs...)
}

// HealthcheckURL is the readiness URL the container health check probes. It only
// needs HTTP_ADDR, so the healthcheck command works without the full config.
func HealthcheckURL() string {
	if u := os.Getenv("HEALTHCHECK_URL"); u != "" {
		return u
	}
	addr := getenv("HTTP_ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr + "/readyz"
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func duration(key, fallback string, errs *[]error) time.Duration {
	d, err := time.ParseDuration(getenv(key, fallback))
	if err != nil || d < 0 {
		*errs = append(*errs, fmt.Errorf("%s must be a non-negative duration (e.g. 3s)", key))
	}
	return d
}
