package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	AppPort string
	AppEnv  string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// DBLogLevel controls GORM's SQL logging: silent, error, warn (default) or info.
	// "info" logs every statement with its parameters and must not be used in production.
	DBLogLevel string
}

var validDBLogLevels = map[string]bool{"silent": true, "error": true, "warn": true, "info": true}

func Load() (*Config, error) {
	// Load .env without overriding existing environment variables.

	if err := godotenv.Load(); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load .env: %w", err)
		}
	}

	required := []string{
		"APP_PORT",
		"DB_HOST",
		"DB_PORT",
		"DB_USER",
		"DB_PASSWORD",
		"DB_NAME",
		"DB_SSLMODE",
	}

	for _, key := range required {
		if os.Getenv(key) == "" {
			return nil, fmt.Errorf("missing required environment variable: %s", key)
		}
	}

	dbLogLevel := os.Getenv("DB_LOG_LEVEL")
	if dbLogLevel == "" {
		dbLogLevel = "warn"
	}
	if !validDBLogLevels[dbLogLevel] {
		return nil, fmt.Errorf("invalid DB_LOG_LEVEL %q: use silent, error, warn or info", dbLogLevel)
	}

	return &Config{
		AppPort:    os.Getenv("APP_PORT"),
		AppEnv:     os.Getenv("APP_ENV"),
		DBHost:     os.Getenv("DB_HOST"),
		DBPort:     os.Getenv("DB_PORT"),
		DBUser:     os.Getenv("DB_USER"),
		DBPassword: os.Getenv("DB_PASSWORD"),
		DBName:     os.Getenv("DB_NAME"),
		DBSSLMode:  os.Getenv("DB_SSLMODE"),
		DBLogLevel: dbLogLevel,
	}, nil
}
