package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                 string
	Environment          string
	DatabaseURL          string
	IdempotencyLockTTL   time.Duration
	IdempotencyRetention time.Duration
	AllowedOrigins       []string
}

func LoadConfig() *Config {
	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env file found, using system environment variables")
	}

	cfg := &Config{
		Port:                 getEnv("PORT", "8080"),
		Environment:          getEnv("ENVIRONMENT", "development"),
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/walletDB?sslmode=disable"),
		IdempotencyLockTTL:   getSecondsEnv("IDEMPOTENCY_LOCK_TTL_SECONDS", 30*time.Second),
		IdempotencyRetention: getSecondsEnv("IDEMPOTENCY_RETENTION_SECONDS", 24*time.Hour),
		// IdempotencyLockTTL:   getDurationEnv("IDEMPOTENCY_LOCK_TTL", 30*time.Second),
		// IdempotencyRetention: getDurationEnv("IDEMPOTENCY_RETENTION", 24*time.Hour),
		AllowedOrigins: getSliceEnv("ALLOWED_ORIGINS", []string{"http://localhost:3000", "http://localhost:8080"}),
	}
	slog.Info("configuration loaded",
		"port", cfg.Port,
		"environment", cfg.Environment,
		"idempotency_lock_ttl", cfg.IdempotencyLockTTL,
		"idempotency_retention", cfg.IdempotencyRetention,
		"allowed_origins", cfg.AllowedOrigins,
	)
	return cfg
}

func getEnv(key, fallback string) string {
	if val, exists := os.LookupEnv(key); exists && val != "" {
		return val
	}
	return fallback
}

func getSliceEnv(key string, fallback []string) []string {
	valStr := os.Getenv(key)
	if valStr == "" {
		return fallback
	}
	var items []string
	for _, item := range strings.Split(valStr, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	if len(items) == 0 {
		return fallback
	}
	return items
}

func getSecondsEnv(key string, fallback time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return fallback
	}
	secs, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil || secs < 0 {
		slog.Warn("invalid seconds for env var, using fallback",
			"key", key,
			"val", valStr,
			"fallback", fallback,
		)
		return fallback
	}
	return time.Duration(secs) * time.Second
}

/* func getDurationEnv(key string, fallback time.Duration) time.Duration {
	valStr := os.Getenv(key)
	if valStr == "" {
		return fallback
	}
	d, err := time.ParseDuration(valStr)
	if err != nil {
		slog.Warn("invalid duration format for env var, using fallback",
			"key", key,
			"val", valStr,
			"fallback", fallback,
		)
		return fallback
	}
	return d
} */
