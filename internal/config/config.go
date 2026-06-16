package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	HTTPPort    int
	LogLevel    string
}

func Load() (Config, error) {
	_ = godotenv.Load() // ignore missing .env in CI/production

	port := 8080
	if v := os.Getenv("HTTP_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid HTTP_PORT: %w", err)
		}
		port = p
	}

	dbURL, err := databaseURL()
	if err != nil {
		return Config{}, err
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	return Config{
		DatabaseURL: dbURL,
		HTTPPort:    port,
		LogLevel:    logLevel,
	}, nil
}

// databaseURL resolves the connection string from DATABASE_URL or individual DB_* variables.
func databaseURL() (string, error) {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v, nil
	}

	host := envOrDefault("DB_HOST", "localhost")
	port := envOrDefault("DB_PORT", "5432")
	user := envOrDefault("DB_USER", "wallet")
	password := os.Getenv("DB_PASSWORD")
	name := envOrDefault("DB_NAME", "wallet_transfer")
	sslmode := envOrDefault("DB_SSLMODE", "disable")

	if password == "" {
		password = envOrDefault("DB_PASSWORD", "wallet")
	}

	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   fmt.Sprintf("%s:%s", host, port),
		Path:   name,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
