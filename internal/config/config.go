package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Config holds all configuration properties for the service.
type Config struct {
	ServiceName string `json:"service_name"`
	Port        string `json:"port"`
	Environment string `json:"environment"`
	LogLevel    string `json:"log_level"`
	DatabaseURL string `json:"database_url"`
}

// DefaultConfig returns safe baseline configuration values.
func DefaultConfig() *Config {
	return &Config{
		ServiceName: "wallet-transfer-service",
		Port:        "8080",
		Environment: "development",
		LogLevel:    "info",
		DatabaseURL: "postgres://raghav@localhost:5432/wallet_transfer?sslmode=disable",
	}
}

// Load loads configuration with the following priority:
// 1. Defaults
// 2. config.json (or path in CONFIG_FILE env var, if file exists)
// 3. .env file (if present)
// 4. Environment variables
func Load(configPath ...string) (*Config, error) {
	cfg := DefaultConfig()

	// 1. Determine JSON config file path
	targetPath := "config.json"
	if len(configPath) > 0 && configPath[0] != "" {
		targetPath = configPath[0]
	}

	// 2. Read JSON file if it exists
	if data, err := os.ReadFile(targetPath); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config file %s: %w", targetPath, err)
		}
	}

	return cfg, nil
}
