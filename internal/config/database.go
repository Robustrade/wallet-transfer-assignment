package config

import (
	"fmt"
	"net/url"
	"os"
)

// Database holds PostgreSQL connection settings.
type Database struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
	SSLMode  string
}

// DSN returns a PostgreSQL connection string.
func (d Database) DSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%s", d.Host, d.Port),
		Path:   d.Name,
	}
	q := u.Query()
	q.Set("sslmode", d.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

// LoadDatabase reads PostgreSQL settings from the environment.
// DATABASE_URL takes precedence when set.
func LoadDatabase() Database {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return DatabaseFromDSN(dsn)
	}

	return Database{
		Host:     envOrDefault("POSTGRES_HOST", "localhost"),
		Port:     envOrDefault("WALLET_DB_HOST_PORT", envOrDefault("POSTGRES_PORT", "5433")),
		User:     envOrDefault("POSTGRES_USER", "wallet"),
		Password: envOrDefault("POSTGRES_PASSWORD", "wallet"),
		Name:     envOrDefault("POSTGRES_DB", "wallet_transfer"),
		SSLMode:  envOrDefault("POSTGRES_SSLMODE", "disable"),
	}
}

// DatabaseFromDSN parses a postgres URL into Database settings.
func DatabaseFromDSN(dsn string) Database {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return Database{SSLMode: "disable"}
	}

	db := Database{
		Host:    parsed.Hostname(),
		SSLMode: parsed.Query().Get("sslmode"),
	}
	if db.SSLMode == "" {
		db.SSLMode = "disable"
	}
	if port := parsed.Port(); port != "" {
		db.Port = port
	} else {
		db.Port = "5432"
	}
	if parsed.User != nil {
		db.User = parsed.User.Username()
		db.Password, _ = parsed.User.Password()
	}
	db.Name = trimLeadingSlash(parsed.Path)
	return db
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func trimLeadingSlash(path string) string {
	if len(path) > 0 && path[0] == '/' {
		return path[1:]
	}
	return path
}
