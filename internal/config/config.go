// Package config loads application settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all application settings.
type Config struct {
	Port string
	DB   DBConfig
}

// DBConfig holds the PostgreSQL connection settings.
type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

// Load reads settings from the environment. If a .env file exists in the
// working directory it is loaded first; variables already set in the real
// environment take precedence over values in the file.
func Load() (Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env file: %w", err)
	}

	var missing []string
	require := func(key string) string {
		value := os.Getenv(key)
		if value == "" {
			missing = append(missing, key)
		}
		return value
	}

	cfg := Config{
		Port: require("PORT"),
		DB: DBConfig{
			Host:     require("DB_HOST"),
			Port:     require("DB_PORT"),
			User:     require("DB_USER"),
			Password: require("DB_PASSWORD"),
			Name:     require("DB_NAME"),
			SSLMode:  require("DB_SSLMODE"),
		},
	}

	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// URL builds a PostgreSQL connection string, escaping special characters.
func (c DBConfig) URL() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(c.User, c.Password),
		Host:     net.JoinHostPort(c.Host, c.Port),
		Path:     c.Name,
		RawQuery: url.Values{"sslmode": {c.SSLMode}}.Encode(),
	}
	return u.String()
}
