package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration sourced from environment variables.
type Config struct {
	Port        string
	DatabaseURL string
	Env         string
}

// Load reads a .env file (if present) and then maps env vars into Config.
// Required variables: DATABASE_URL.
func Load() (*Config, error) {
	// Best-effort: load .env when running locally; ignore error in production.
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	return &Config{
		Port:        port,
		DatabaseURL: dbURL,
		Env:         env,
	}, nil
}
