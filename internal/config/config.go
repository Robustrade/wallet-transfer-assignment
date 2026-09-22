// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all environment-derived settings for the service.
type Config struct {
	// Port is the TCP port the HTTP server listens on.
	Port string
	// DatabaseURL is a Postgres connection string, e.g.
	// postgres://user:pass@host:5432/dbname?sslmode=disable
	DatabaseURL string
	// LogLevel is one of debug, info, warn, error.
	LogLevel string
	// MaxClaimAttempts bounds retries while claiming an idempotency key.
	MaxClaimAttempts int
	// SSLEnabled controls whether the server serves HTTPS instead of HTTP.
	SSLEnabled bool
	// TLSCertFile and TLSKeyFile configure the HTTPS certificate and key. When
	// SSL is enabled and both are empty, an ephemeral certificate is used.
	TLSCertFile string
	TLSKeyFile  string
}

// Load reads configuration from the environment, applying sane defaults for
// local development and returning an error if required values are missing.
func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		TLSCertFile: getEnv("TLS_CERT_FILE", ""),
		TLSKeyFile:  getEnv("TLS_KEY_FILE", ""),
	}
	maxClaimAttempts, err := strconv.Atoi(getEnv("MAX_CLAIM_ATTEMPTS", "3"))
	if err != nil || maxClaimAttempts < 1 {
		return Config{}, fmt.Errorf("MAX_CLAIM_ATTEMPTS must be a positive integer")
	}
	cfg.MaxClaimAttempts = maxClaimAttempts
	sslEnabled, err := strconv.ParseBool(getEnv("SSL_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("SSL_ENABLED must be a boolean: %w", err)
	}
	cfg.SSLEnabled = sslEnabled
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SSLEnabled && (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
		return Config{}, fmt.Errorf("TLS_CERT_FILE and TLS_KEY_FILE must be provided together")
	}
	return cfg, nil
}

// TLSConfigured reports whether SSL is enabled for the server.
func (c Config) TLSConfigured() bool {
	return c.SSLEnabled
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
