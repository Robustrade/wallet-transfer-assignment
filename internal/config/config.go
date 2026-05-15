package config

import "os"

type Config struct {
	Port   string
	DBURL  string
	AppEnv string
}

func Load() *Config {
	return &Config{
		Port:   getEnv("PORT", "9000"),
		DBURL:  getEnv("DB_URL", "postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"),
		AppEnv: getEnv("APP_ENV", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
