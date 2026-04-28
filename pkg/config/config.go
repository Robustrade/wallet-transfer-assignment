package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration.
type Config struct {
	DBURL          string
	BindingAddress string
	LogLevel       int
}

var Instance *Config

// Load reads configuration from cmd/.env and then environment variables.
func Load() error {
	if err := godotenv.Load("cmd/.env"); err != nil {
		log.Println("no cmd/.env file found, using environment variables")
	}

	Instance = &Config{
		DBURL:          getEnv("DB_URL", "postgres://wallet_user:wallet_pass@localhost:5432/wallet_db"),
		BindingAddress: getEnv("BINDING_ADDRESS", "0.0.0.0:8080"),
		LogLevel:       parseInt(getEnv("LOG_LEVEL", "0"), 0),
	}

	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
