package config

import "os"

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	MigrationsDir string
}

func Load() Config {
	return Config{
		HTTPAddr:      getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:   getEnv("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"),
		MigrationsDir: getEnv("MIGRATIONS_DIR", "migrations"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
