// Package config binds application configuration from environment
// variables and validates it before any dependent component starts.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
)

// Config holds the environment-sourced settings the server needs to start.
type Config struct {
	WorkerID    int64         `env:"WORKER_ID"`
	PostgresDSN string        `env:"POSTGRES_DSN"`
	RedisAddr   string        `env:"REDIS_ADDR"`
	Port        string        `env:"PORT"`
	LockTimeout time.Duration `env:"LOCK_TIMEOUT"`
}

// Load reads .env (if present) and then the process environment into a Config.
func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// Validate checks that every required field is present and well-formed.
// It must be called before any connection is attempted.
func (c *Config) Validate() error {
	if c.PostgresDSN == "" {
		return fmt.Errorf("POSTGRES_DSN is required")
	}
	if c.RedisAddr == "" {
		return fmt.Errorf("REDIS_ADDR is required")
	}
	if c.Port == "" {
		return fmt.Errorf("PORT is required")
	}
	if c.LockTimeout <= 0 {
		return fmt.Errorf("LOCK_TIMEOUT must be a positive duration")
	}
	if _, err := idgen.NewGenerator(c.WorkerID); err != nil {
		return fmt.Errorf("WORKER_ID invalid: %w", err)
	}
	return nil
}
