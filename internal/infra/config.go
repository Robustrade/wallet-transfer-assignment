package infra

import "os"

type Config struct {
	Port        string
	DatabaseURL string
}

func LoadConfig() Config {
	return Config{
		Port:        envOrDefault("PORT", "8080"),
		DatabaseURL: envOrDefault("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"),
	}
}

func envOrDefault(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
