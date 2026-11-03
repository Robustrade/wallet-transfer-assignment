package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
)

// Config contains the PostgreSQL connection settings read by the application.
type Config struct {
	DatabaseURL     string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// LoadConfig reads database settings from the process environment.
func LoadConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return Config{}, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if (parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql") ||
		parsedURL.Host == "" || parsedURL.Path == "" || parsedURL.Path == "/" {
		return Config{}, fmt.Errorf("DATABASE_URL must be a PostgreSQL URL with a host and database name")
	}

	maxOpen, err := positiveIntEnv("DB_MAX_OPEN_CONNS", defaultMaxOpenConns)
	if err != nil {
		return Config{}, err
	}
	maxIdle, err := positiveIntEnv("DB_MAX_IDLE_CONNS", defaultMaxIdleConns)
	if err != nil {
		return Config{}, err
	}
	if maxIdle > maxOpen {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS cannot exceed DB_MAX_OPEN_CONNS")
	}

	lifetime := defaultConnMaxLifetime
	if raw := os.Getenv("DB_CONN_MAX_LIFETIME"); raw != "" {
		lifetime, err = time.ParseDuration(raw)
		if err != nil || lifetime <= 0 {
			return Config{}, fmt.Errorf("DB_CONN_MAX_LIFETIME must be a positive duration")
		}
	}

	return Config{
		DatabaseURL:     databaseURL,
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: lifetime,
	}, nil
}

// Open creates and verifies a database connection pool. The caller must
// register the PostgreSQL driver (for example, pgx stdlib) before calling Open.
func Open(ctx context.Context, driverName string, cfg Config) (*sql.DB, error) {
	if driverName == "" {
		return nil, fmt.Errorf("database driver name is required")
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	if cfg.MaxOpenConns <= 0 || cfg.MaxIdleConns < 0 ||
		cfg.MaxIdleConns > cfg.MaxOpenConns || cfg.ConnMaxLifetime <= 0 {
		return nil, fmt.Errorf("invalid database pool configuration")
	}

	db, err := sql.Open(driverName, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}
