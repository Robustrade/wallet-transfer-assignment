package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	DefaultListenAddr     = ":8080"
	DefaultLogLevel       = "INFO"
	DefaultDBMaxOpenConns = 25
	DefaultDBMaxIdleConns = 5
)

// Config holds application configuration loaded from the environment.
type Config struct {
	DatabaseURL    string
	ListenAddr     string
	LogLevel       string
	DBMaxOpenConns int
	DBMaxIdleConns int
}

// LoadConfig reads environment variables and returns a validated config.
func LoadConfig() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = DefaultListenAddr
	}

	logLevel := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if logLevel == "" {
		logLevel = DefaultLogLevel
	}

	maxOpenConns, err := parseIntEnv("DB_MAX_OPEN_CONNS", DefaultDBMaxOpenConns)
	if err != nil {
		return Config{}, err
	}

	maxIdleConns, err := parseIntEnv("DB_MAX_IDLE_CONNS", DefaultDBMaxIdleConns)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:    databaseURL,
		ListenAddr:     listenAddr,
		LogLevel:       logLevel,
		DBMaxOpenConns: maxOpenConns,
		DBMaxIdleConns: maxIdleConns,
	}, nil
}

func parseIntEnv(key string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("invalid %s: must be >= 0", key)
	}
	return parsed, nil
}
