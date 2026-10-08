package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	DBMaxOpenConns     int
	DBMaxIdleConns     int
	DBConnMaxLifetime  time.Duration
	DBConnMaxIdleTime  time.Duration
	HTTPRequestTimeout time.Duration
	StartupTimeout     time.Duration
	ShutdownTimeout    time.Duration
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), HTTPAddr: ":8080", DBMaxOpenConns: 10, DBMaxIdleConns: 5, DBConnMaxLifetime: 30 * time.Minute, DBConnMaxIdleTime: 5 * time.Minute, HTTPRequestTimeout: 10 * time.Second, StartupTimeout: 5 * time.Second, ShutdownTimeout: 10 * time.Second}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if v, ok := os.LookupEnv("HTTP_ADDR"); ok {
		if strings.TrimSpace(v) == "" {
			return Config{}, fmt.Errorf("HTTP_ADDR must not be empty")
		}
		c.HTTPAddr = v
	}
	for _, setting := range []struct {
		name string
		dest *int
		min  int
	}{
		{"DB_MAX_OPEN_CONNS", &c.DBMaxOpenConns, 1}, {"DB_MAX_IDLE_CONNS", &c.DBMaxIdleConns, 0},
	} {
		if v, ok := os.LookupEnv(setting.name); ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < setting.min {
				return Config{}, fmt.Errorf("%s must be an integer >= %d", setting.name, setting.min)
			}
			*setting.dest = n
		}
	}
	if c.DBMaxIdleConns > c.DBMaxOpenConns {
		return Config{}, fmt.Errorf("DB_MAX_IDLE_CONNS must not exceed DB_MAX_OPEN_CONNS")
	}
	for _, setting := range []struct {
		name string
		dest *time.Duration
	}{
		{"DB_CONN_MAX_LIFETIME", &c.DBConnMaxLifetime}, {"DB_CONN_MAX_IDLE_TIME", &c.DBConnMaxIdleTime}, {"HTTP_REQUEST_TIMEOUT", &c.HTTPRequestTimeout}, {"STARTUP_TIMEOUT", &c.StartupTimeout}, {"SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
	} {
		if v, ok := os.LookupEnv(setting.name); ok {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				return Config{}, fmt.Errorf("%s must be a positive duration", setting.name)
			}
			*setting.dest = d
		}
	}
	return c, nil
}
