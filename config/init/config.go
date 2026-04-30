// Package config exposes LoadConfig, which reads service configuration
// from environment variables and constructs ready-to-use infrastructure clients.
package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/config/infra"
)

// CommonConfig holds the cross-cutting service settings.
type CommonConfig struct {
	Port            string
	ShutdownTimeout time.Duration
}

// ServiceInstances aggregates the runtime instances passed to service initializers.
type ServiceInstances struct {
	Common CommonConfig
	Logger *slog.Logger
	DB     *gorm.DB
}

// LoadConfig reads environment variables, opens a database connection, and
// returns the wired ServiceInstances. The returned instance carries a Close
// method for graceful shutdown of the DB pool.
func LoadConfig(ctx context.Context) (*ServiceInstances, error) {
	shutdownTimeout, err := envDurationOrDefault("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return nil, err
	}
	common := CommonConfig{
		Port:            envOrDefault("PORT", "8080"),
		ShutdownTimeout: shutdownTimeout,
	}

	logJSON, err := envBool("LOG_JSON", true)
	if err != nil {
		return nil, err
	}
	logger := infra.InitLogger(infra.LoggerConfig{
		Level: os.Getenv("LOG_LEVEL"),
		JSON:  logJSON,
	})

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("config: DATABASE_URL is required")
	}

	// DB_MAX_OPEN_CONNS default is intentionally conservative. Concurrent
	// transfers serialize on the per-wallet row lock anyway, so a larger pool
	// does not raise throughput on hot wallets — it just lets more goroutines
	// hold connections while waiting on locks. Tune up via env when running
	// against a workload with many independent wallets in parallel.
	maxOpenConns, err := envIntOrDefault("DB_MAX_OPEN_CONNS", 10)
	if err != nil {
		return nil, err
	}
	maxIdleConns, err := envIntOrDefault("DB_MAX_IDLE_CONNS", 5)
	if err != nil {
		return nil, err
	}
	connMaxLifetime, err := envDurationOrDefault("DB_CONN_MAX_LIFETIME", 30*time.Minute)
	if err != nil {
		return nil, err
	}
	pingTimeout, err := envDurationOrDefault("DB_PING_TIMEOUT", 5*time.Second)
	if err != nil {
		return nil, err
	}

	db, err := infra.InitPostgres(ctx, infra.PostgresConfig{
		DSN:             dsn,
		MaxOpenConns:    maxOpenConns,
		MaxIdleConns:    maxIdleConns,
		ConnMaxLifetime: connMaxLifetime,
		PingTimeout:     pingTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &ServiceInstances{
		Common: common,
		Logger: logger,
		DB:     db,
	}, nil
}

// Close releases resources held by ServiceInstances.
func (s *ServiceInstances) Close() error {
	if s == nil {
		return nil
	}
	return infra.ClosePostgres(s.DB)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	out, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s is not a valid integer (%q): %w", key, v, err)
	}
	return out, nil
}

func envDurationOrDefault(key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s is not a valid duration (%q): %w", key, v, err)
	}
	return d, nil
}

func envBool(key string, def bool) (bool, error) {
	raw := os.Getenv(key)
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return def, nil
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("config: %s is not a valid bool (%q)", key, raw)
	}
}
