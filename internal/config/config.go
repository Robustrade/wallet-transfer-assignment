// Package config loads runtime configuration from the environment.
package config

import (
	"errors"
	"os"
	"strings"
)

// Config is the resolved server configuration.
type Config struct {
	DatabaseURL string
	Port        string
}

// Load reads configuration from the environment. DATABASE_URL is required;
// PORT defaults to 8080. Surrounding quotes are stripped defensively so a
// quoted .env value works even when the binary is run outside `make`.
func Load() (Config, error) {
	dsn := strings.Trim(os.Getenv("DATABASE_URL"), `"'`)
	if dsn == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	return Config{DatabaseURL: dsn, Port: port}, nil
}
