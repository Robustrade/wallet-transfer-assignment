package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Database    DatabaseConfig `json:"database"`
	Server      ServerConfig   `json:"server"`
	Environment string         `json:"environment"`
}

type DatabaseConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	SSLMode  string `json:"sslmode"`
}

type ServerConfig struct {
	Port string `json:"port"`
}

// Load reads configuration from config.json file
func Load(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config.json"
	}

	file, err := os.Open(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	var config Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}

	return &config, nil
}

// ConnectionString returns PostgreSQL connection string
func (d *DatabaseConfig) ConnectionString() string {
	// For production, password should always be required
	// For local development without password, we allow it but only if explicitly empty
	if d.Password == "" {
		// Development mode: allow peer authentication (no password)
		return fmt.Sprintf(
			"host=%s port=%s user=%s dbname=%s sslmode=%s",
			d.Host, d.Port, d.User, d.DBName, d.SSLMode,
		)
	}

	// Production mode: use password
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.DBName, d.SSLMode,
	)
}
