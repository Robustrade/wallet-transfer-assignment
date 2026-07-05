// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all environment-driven settings for the service.
type Config struct {
	ServerPort      string
	DBHost          string
	DBPort          string
	DBUser          string
	DBPassword      string
	DBName          string
	DBSSLMode       string
	ShutdownTimeout time.Duration
}

// Load reads configuration from environment variables, falling back to
// sane local-development defaults for anything unset.
func Load() Config {
	return Config{
		ServerPort:      getEnv("SERVER_PORT", "8080"),
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          getEnv("DB_PORT", "5432"),
		DBUser:          getEnv("DB_USER", "wallet"),
		DBPassword:      getEnv("DB_PASSWORD", "wallet"),
		DBName:          getEnv("DB_NAME", "wallet_transfer"),
		DBSSLMode:       getEnv("DB_SSLMODE", "disable"),
		ShutdownTimeout: 10 * time.Second,
	}
}

// DSN builds a Postgres connection string from the config.
func (c Config) DSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
