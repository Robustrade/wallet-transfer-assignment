package config

import (
	"fmt"
	"os"
)

const (
	DB_URL = "DB_URL"
)

type PostgresConfig struct {
	DbUrl string
}

func LoadPostgresConfig() (PostgresConfig, error) {
	dbUrl, ok := os.LookupEnv(DB_URL)
	if !ok {
		return PostgresConfig{}, fmt.Errorf("Env Var %s not set", DB_URL)
	}

	return PostgresConfig{
		DbUrl: dbUrl,
	}, nil
}
