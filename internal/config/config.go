package config

import "os"

// Config holds all runtime configuration read from environment variables.
type Config struct {
	DBDriver    string // "sqlite" or "postgres"
	DatabaseURL string // file path (sqlite) or connection string (postgres)
	Port        string // HTTP listen port
	LogLevel    string // "debug", "info", "warn", "error"
	LogFormat   string // "text" or "json"
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	return &Config{
		DBDriver:    getEnv("DB_DRIVER", "sqlite"),
		DatabaseURL: getEnv("DATABASE_URL", "wallet_transfer.db"),
		Port:        getEnv("PORT", "8080"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		LogFormat:   getEnv("LOG_FORMAT", "text"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
