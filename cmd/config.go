package cmd

import (
	"fmt"
	"os"
)

type Config struct {
	DATABASE_URL string
}

func LoadConfig() (*Config, error) {
	config := &Config{}

	config.DATABASE_URL = os.Getenv("DATABASE_URL")
	if config.DATABASE_URL == "" {
		return nil, fmt.Errorf("missing database url from config")
	}

	return config, nil
}
