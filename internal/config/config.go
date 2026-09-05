package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Env            string
	HTTPAddr       string
	DatabaseURL    string
	LogLevel       string
	RequestTimeout time.Duration
	MigrationsDir  string
}

const EnvDevelopment = "development"

func (c Config) IsDevelopment() bool {
	return strings.EqualFold(c.Env, EnvDevelopment) || strings.EqualFold(c.Env, "dev")
}

func Load() (Config, error) {
	cfg := Config{
		Env:            getEnv("APP_ENV", "production"),
		HTTPAddr:       getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"),
		LogLevel:       getEnv("LOG_LEVEL", "info"),
		RequestTimeout: getEnvDuration("REQUEST_TIMEOUT", 5*time.Second),
		MigrationsDir:  getEnv("MIGRATIONS_DIR", "migrations"),
	}

	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL must not be empty")
	}
	if c.RequestTimeout <= 0 {
		return fmt.Errorf("REQUEST_TIMEOUT must be positive")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return fallback
}
