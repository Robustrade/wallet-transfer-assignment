// Package config provides application configuration from environment variables.
package config

import (
	"os"
)

// Config holds all application configuration.
type Config struct {
	DatabaseURL string
	Port        string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		DatabaseURL: getEnv("DATABASE_URL", "postgres://wallet_user:wallet_pass@localhost:5432/wallet_db?sslmode=disable"),
		Port:        getEnv("PORT", "8080"),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
