package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

const (
	POSTGRES_USER     = "POSTGRES_USER"
	POSTGRES_PASSWORD = "POSTGRES_PASSWORD"
	POSTGRES_DB       = "POSTGRES_DB"
	POSTGRES_HOST     = "POSTGRES_HOST"
	POSTGRES_PORT     = "POSTGRES_PORT"
)

type PostgresConfig struct {
	User     string
	Password string
	DB       string
	Host     string
	Port     string
}

func LoadPostgresConfig() (PostgresConfig, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return PostgresConfig{}, fmt.Errorf("failed to load .env file: %w", err)
	}

	user, exists := os.LookupEnv(POSTGRES_USER)
	if !exists {
		return PostgresConfig{}, fmt.Errorf("environment variable %s not set", POSTGRES_USER)
	}

	passwd, exists := os.LookupEnv(POSTGRES_PASSWORD)
	if !exists {
		return PostgresConfig{}, fmt.Errorf("environment variable %s not set", POSTGRES_PASSWORD)
	}

	db, exists := os.LookupEnv(POSTGRES_DB)
	if !exists {
		return PostgresConfig{}, fmt.Errorf("environment variable %s not set", POSTGRES_DB)
	}

	host, exists := os.LookupEnv(POSTGRES_HOST)
	if !exists {
		return PostgresConfig{}, fmt.Errorf("environment variable %s not set", POSTGRES_HOST)
	}

	port, exists := os.LookupEnv(POSTGRES_PORT)
	if !exists {
		return PostgresConfig{}, fmt.Errorf("environment variable %s not set", POSTGRES_PORT)
	}

	return PostgresConfig{
		User:     user,
		Password: passwd,
		DB:       db,
		Host:     host,
		Port:     port,
	}, nil
}
