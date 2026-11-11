package config

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"wallet-transfer-assignment/internal/appErrors"
)

// Config contains startup settings. Never log it: DatabaseURL contains credentials.
type Config struct {
	DatabaseURL       string
	Host              string
	Port              string
	ShutdownTimeout   time.Duration
	OperationTimeout  time.Duration
	MaxTransferAmount int64
}

var loadOnce = sync.OnceValues(func() (Config, error) { return Load(os.Getenv) })

// Get reads and validates the environment once. It returns a copy of the config
// and caches initialization errors as well; configuration changes need a restart.
func Get() (Config, error) { return loadOnce() }

// Load parses explicit settings without changing the process singleton.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: getenv("DATABASE_URL"),
		Host:        value(getenv("HTTP_HOST"), "127.0.0.1"),
		Port:        value(getenv("HTTP_PORT"), "8080"),
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, appErrors.ErrDatabaseURLRequired
	}

	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, appErrors.ErrInvalidPort
	}

	cfg.ShutdownTimeout, err = time.ParseDuration(value(getenv("HTTP_SHUTDOWN_TIMEOUT"), "15s"))
	if err != nil || cfg.ShutdownTimeout <= 0 {
		return Config{}, appErrors.ErrInvalidShutdownTimeout
	}

	cfg.OperationTimeout, err = time.ParseDuration(value(getenv("DB_OPERATION_TIMEOUT"), "5s"))
	if err != nil || cfg.OperationTimeout <= 0 {
		return Config{}, appErrors.ErrInvalidOperationTimeout
	}
	cfg.MaxTransferAmount, err = strconv.ParseInt(value(getenv("MAX_TRANSFER_AMOUNT"), "1000000000000"), 10, 64)
	if err != nil || cfg.MaxTransferAmount <= 0 {
		return Config{}, appErrors.ErrInvalidMaxTransferAmount
	}
	return cfg, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, c.Port)
}

func value(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
