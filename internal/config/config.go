package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL      string
	Port             string
	SSLEnabled       bool
	TLSCertFile      string
	TLSKeyFile       string
	MaxClaimAttempts int
}

func Load() (Config, error) {
	return LoadFromEnv(os.Getenv)
}

func LoadFromEnv(getenv func(string) string) (Config, error) {
	config := Config{
		DatabaseURL:      strings.TrimSpace(getenv("DATABASE_URL")),
		Port:             strings.TrimSpace(getenv("PORT")),
		TLSCertFile:      strings.TrimSpace(getenv("TLS_CERT_FILE")),
		TLSKeyFile:       strings.TrimSpace(getenv("TLS_KEY_FILE")),
		MaxClaimAttempts: 3,
	}
	if config.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if config.Port == "" {
		config.Port = "8080"
	}
	port, err := strconv.Atoi(config.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("PORT must be an integer between 1 and 65535")
	}
	if raw := strings.TrimSpace(getenv("MAX_CLAIM_ATTEMPTS")); raw != "" {
		config.MaxClaimAttempts, err = strconv.Atoi(raw)
		if err != nil || config.MaxClaimAttempts < 1 || config.MaxClaimAttempts > 100 {
			return Config{}, errors.New("MAX_CLAIM_ATTEMPTS must be an integer between 1 and 100")
		}
	}
	if raw := strings.TrimSpace(getenv("SSL_ENABLED")); raw != "" {
		config.SSLEnabled, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SSL_ENABLED must be true or false: %w", err)
		}
	}
	if (config.TLSCertFile == "") != (config.TLSKeyFile == "") {
		return Config{}, errors.New("TLS_CERT_FILE and TLS_KEY_FILE must be provided together")
	}
	return config, nil
}
