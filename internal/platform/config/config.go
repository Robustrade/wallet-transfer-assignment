package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL string
	Port        int
}

func FromEnv() (Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	port := 8080
	if raw := os.Getenv("PORT"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("PORT must be an int: %w", err)
		}
		port = v
	}

	return Config{
		DatabaseURL: dbURL,
		Port:        port,
	}, nil
}
