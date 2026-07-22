// Package config loads service configuration via viper (defaults -> config file
// -> environment overrides).
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration. Every value is config-driven: it can
// be set by an optional config.yaml or overridden by an env var of the same
// name in upper snake case (e.g. HTTP_ADDR, LOG_LEVEL, DATABASE_URL).
type Config struct {
	HTTPAddr        string        `mapstructure:"http_addr"`
	DatabaseURL     string        `mapstructure:"database_url"`
	MaxDBConns      int32         `mapstructure:"db_max_conns"`
	LogLevel        string        `mapstructure:"log_level"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// Async worker tuning.
	WorkerCount        int           `mapstructure:"worker_count"`
	WorkerBatchSize    int           `mapstructure:"worker_batch_size"`
	WorkerPollInterval time.Duration `mapstructure:"worker_poll_interval"`
	ReaperInterval     time.Duration `mapstructure:"reaper_interval"`
	StuckAfter         time.Duration `mapstructure:"stuck_after"`
	MaxAttempts        int           `mapstructure:"max_attempts"`

	// Tracing (OpenTelemetry). Disabled by default.
	ServiceName      string  `mapstructure:"service_name"`
	TracingEnabled   bool    `mapstructure:"tracing_enabled"`
	OTLPEndpoint     string  `mapstructure:"otlp_endpoint"`
	TraceSampleRatio float64 `mapstructure:"trace_sample_ratio"`

	// Used to assemble DatabaseURL when it is not supplied directly.
	DBHost     string `mapstructure:"db_host"`
	DBPort     string `mapstructure:"db_port"`
	DBUser     string `mapstructure:"db_user"`
	DBPassword string `mapstructure:"db_password"`
	DBName     string `mapstructure:"db_name"`
}

// Load resolves configuration from defaults, an optional config.yaml, and env
// overrides. Env wins so containers can be configured without a file.
func Load() (Config, error) {
	v := viper.New()

	v.SetDefault("http_addr", ":8080")
	v.SetDefault("database_url", "")
	v.SetDefault("db_max_conns", 20)
	v.SetDefault("log_level", "info")
	v.SetDefault("shutdown_timeout", "15s")
	v.SetDefault("worker_count", 4)
	v.SetDefault("worker_batch_size", 10)
	v.SetDefault("worker_poll_interval", "500ms")
	v.SetDefault("reaper_interval", "30s")
	v.SetDefault("stuck_after", "60s")
	v.SetDefault("max_attempts", 5)
	v.SetDefault("service_name", "wallet-transfer")
	v.SetDefault("tracing_enabled", false)
	v.SetDefault("otlp_endpoint", "localhost:4317")
	v.SetDefault("trace_sample_ratio", 1.0)
	v.SetDefault("db_host", "localhost")
	v.SetDefault("db_port", "5432")
	v.SetDefault("db_user", "myuser")
	v.SetDefault("db_password", "mypassword")
	v.SetDefault("db_name", "mydb")

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./config")
	if err := v.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
	}

	// Map env vars (HTTP_ADDR) onto keys (http_addr) and bind each so Unmarshal
	// observes them.
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, key := range v.AllKeys() {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName,
		)
	}
	return cfg, nil
}
