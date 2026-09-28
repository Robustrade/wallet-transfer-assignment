// Package config loads service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Supported values for DB_DRIVER.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// Defaults applied when a variable is unset or empty.
const (
	defaultPort           = 8080
	defaultDriver         = DriverSQLite
	defaultSQLiteDSN      = "file:wallet.db"
	defaultRequestTimeout = 5 * time.Second
	defaultLockTimeout    = 5 * time.Second
	defaultMaxBodyBytes   = 1 << 20 // 1 MiB
)

// Config holds all runtime settings for the service.
type Config struct {
	Port           int
	DBDriver       string
	DBDSN          string
	RequestTimeout time.Duration
	LockTimeout    time.Duration // Postgres lock_timeout / SQLite busy_timeout
	MaxBodyBytes   int64
}

// Load reads configuration through lookup, which has the signature of
// os.LookupEnv. Taking it as a parameter keeps tests independent of the real
// process environment.
//
// Every invalid variable is reported in a single joined error so that a bad
// deployment can be fixed in one pass.
func Load(lookup func(string) (string, bool)) (Config, error) {
	// get returns the variable's value, or "" when it is unset. Unset and
	// empty are treated the same: both mean "use the default".
	get := func(key string) string {
		v, _ := lookup(key)
		return v
	}

	cfg := Config{
		Port:           defaultPort,
		DBDriver:       defaultDriver,
		RequestTimeout: defaultRequestTimeout,
		LockTimeout:    defaultLockTimeout,
		MaxBodyBytes:   defaultMaxBodyBytes,
	}
	var errs []error

	if v := get("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", v))
		} else {
			cfg.Port = port
		}
	}

	if v := get("DB_DRIVER"); v != "" {
		cfg.DBDriver = v
	}
	switch cfg.DBDriver {
	case DriverSQLite, DriverPostgres:
	default:
		errs = append(errs, fmt.Errorf("DB_DRIVER must be %q or %q, got %q",
			DriverSQLite, DriverPostgres, cfg.DBDriver))
	}

	// The SQLite default points at a local file. There is no sensible default
	// for Postgres, so a missing DSN there is a configuration error rather
	// than a confusing connection failure later.
	cfg.DBDSN = get("DB_DSN")
	if cfg.DBDSN == "" {
		if cfg.DBDriver == DriverPostgres {
			errs = append(errs, errors.New("DB_DSN is required when DB_DRIVER is postgres"))
		} else {
			cfg.DBDSN = defaultSQLiteDSN
		}
	}

	if d, err := positiveDuration(get("REQUEST_TIMEOUT"), defaultRequestTimeout); err != nil {
		errs = append(errs, fmt.Errorf("REQUEST_TIMEOUT %w", err))
	} else {
		cfg.RequestTimeout = d
	}

	if d, err := positiveDuration(get("LOCK_TIMEOUT"), defaultLockTimeout); err != nil {
		errs = append(errs, fmt.Errorf("LOCK_TIMEOUT %w", err))
	} else {
		cfg.LockTimeout = d
	}

	if v := get("MAX_BODY_BYTES"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("MAX_BODY_BYTES must be a positive integer, got %q", v))
		} else {
			cfg.MaxBodyBytes = n
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// positiveDuration parses a Go duration string such as "5s" or "250ms".
// An empty string yields def. Bare numbers ("5") are rejected because the
// unit would be ambiguous.
func positiveDuration(v string, def time.Duration) (time.Duration, error) {
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("must be a positive duration like \"5s\", got %q", v)
	}
	return d, nil
}
