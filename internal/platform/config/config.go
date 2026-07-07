package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddress     string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
}

func Load() (Config, error) {
	config := Config{
		HTTPAddress:     envOrDefault("HTTP_ADDRESS", ":8080"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ShutdownTimeout: durationEnvOrDefault("SHUTDOWN_TIMEOUT_SECONDS", 10*time.Second),
		ReadTimeout:     durationEnvOrDefault("HTTP_READ_TIMEOUT_SECONDS", 5*time.Second),
		WriteTimeout:    durationEnvOrDefault("HTTP_WRITE_TIMEOUT_SECONDS", 10*time.Second),
		IdleTimeout:     durationEnvOrDefault("HTTP_IDLE_TIMEOUT_SECONDS", 60*time.Second),
	}
	if config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationEnvOrDefault(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
