// Package config loads runtime configuration from environment variables.
//
// The package is intentionally tiny — a single Config struct and a
// Load function. There is no support for config files or remote
// config sources because the running environment (12-factor style,
// docker compose, Kubernetes) already supplies env vars, and adding
// more sources would muddy the precedence rules without earning any
// real flexibility.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the fully resolved runtime configuration. cmd/server
// constructs a Config via Load at startup and passes it into run().
// Tests build a Config literal directly.
type Config struct {
	// DatabaseURL is the Postgres DSN. Required.
	// Format: postgres://user:pass@host:port/db?sslmode=disable
	DatabaseURL string

	// HTTPAddr is the listen address of the public API server.
	// Defaults to ":8080" if not set.
	HTTPAddr string

	// DebugAddr controls the optional pprof admin listener. Empty
	// means disabled — the default — so pprof is NOT exposed unless
	// the operator explicitly opts in. If set, it should normally
	// be a loopback address like "127.0.0.1:6060". See the security
	// note on internal/handler/debug.go and AGENTS.md §S-16.
	DebugAddr string

	// DBMaxOpenConns caps the number of concurrent database
	// connections the pool will open. Default 25. Tune up with
	// caution — Postgres `max_connections` is finite and shared
	// across every consumer of the cluster. Set DB_MAX_OPEN_CONNS
	// to override.
	DBMaxOpenConns int

	// DBMaxIdleConns is how many connections the pool keeps warm
	// between requests. Default 5. Setting this < DBMaxOpenConns
	// means the pool closes excess connections when traffic drops.
	DBMaxIdleConns int

	// DBConnMaxLifetime is the maximum age of any connection before
	// the pool retires it. Default 5m. Set lower than your load
	// balancer's or pgbouncer's idle-disconnect threshold so we do
	// not try to use a half-closed connection.
	DBConnMaxLifetime time.Duration

	// DBConnMaxIdleTime is how long a connection can sit idle before
	// the pool closes it. Default 1m. Frees Postgres connections
	// during traffic lulls.
	DBConnMaxIdleTime time.Duration
}

// Pool tuning defaults. Sized for a single replica handling
// moderate write load (200–500 req/s). At higher rates, raise
// DBMaxOpenConns in lock-step with Postgres's max_connections.
const (
	DefaultDBMaxOpenConns    = 25
	DefaultDBMaxIdleConns    = 5
	DefaultDBConnMaxLifetime = 5 * time.Minute
	DefaultDBConnMaxIdleTime = 1 * time.Minute
)

// Load reads configuration from environment variables.
//
// Env vars:
//
//	DATABASE_URL           required, Postgres DSN
//	HTTP_ADDR              optional, defaults to ":8080"
//	DEBUG_ADDR             optional, empty by default. Set to
//	                       "127.0.0.1:6060" to enable the pprof admin
//	                       listener on loopback.
//	DB_MAX_OPEN_CONNS      optional int, default 25
//	DB_MAX_IDLE_CONNS      optional int, default 5
//	DB_CONN_MAX_LIFETIME   optional duration (e.g. "5m"), default 5m
//	DB_CONN_MAX_IDLE_TIME  optional duration (e.g. "1m"), default 1m
//
// Returns an error only when DATABASE_URL is missing or when a
// supplied env var fails to parse as the documented type. Numeric
// and duration parse errors surface explicitly rather than silently
// reverting to the default — typos should fail loudly at boot, not
// produce mysterious capacity behaviour.
func Load() (Config, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	debugAddr := os.Getenv("DEBUG_ADDR") // empty = disabled

	maxOpen, err := envInt("DB_MAX_OPEN_CONNS", DefaultDBMaxOpenConns)
	if err != nil {
		return Config{}, err
	}
	maxIdle, err := envInt("DB_MAX_IDLE_CONNS", DefaultDBMaxIdleConns)
	if err != nil {
		return Config{}, err
	}
	maxLife, err := envDuration("DB_CONN_MAX_LIFETIME", DefaultDBConnMaxLifetime)
	if err != nil {
		return Config{}, err
	}
	maxIdleTime, err := envDuration("DB_CONN_MAX_IDLE_TIME", DefaultDBConnMaxIdleTime)
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:       dbURL,
		HTTPAddr:          addr,
		DebugAddr:         debugAddr,
		DBMaxOpenConns:    maxOpen,
		DBMaxIdleConns:    maxIdle,
		DBConnMaxLifetime: maxLife,
		DBConnMaxIdleTime: maxIdleTime,
	}, nil
}

// envInt reads a positive integer from the env var or returns the
// default when unset. Returns an error for parse failures or
// non-positive values; we never silently fall back when the operator
// has typed something — that hides bugs.
func envInt(key string, def int) (int, error) {
	s := os.Getenv(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %d", key, n)
	}
	return n, nil
}

// envDuration reads a Go duration string (e.g. "5m", "30s") or
// returns the default. Same fail-loud-on-parse-error stance as
// envInt.
func envDuration(key string, def time.Duration) (time.Duration, error) {
	s := os.Getenv(key)
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %s", key, d)
	}
	return d, nil
}
