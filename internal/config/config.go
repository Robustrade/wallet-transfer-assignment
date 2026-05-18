package config

import (
	"encoding/json"
	"log"
	"os"

	"github.com/candidate/wallet-transfer/internal/utils"
)

type Config struct {
	DBPath     string `json:"db_path"`
	ServerPort string `json:"server_port"`
	RedisAddr  string `json:"redis_addr"`
}

func Load() *Config {
	file, err := os.ReadFile("config.json")
	if err != nil {
		return &Config{
			DBPath:     utils.GetEnv("DB_PATH", "wallet.db"),
			ServerPort: utils.GetEnv("SERVER_PORT", "8080"),
			RedisAddr:  utils.GetEnv("REDIS_ADDR", "localhost:6379"),
		}
	}

	var cfg Config
	if err := json.Unmarshal(file, &cfg); err != nil {
		log.Fatalf("failed to parse config.json: %v", err)
	}
	
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		cfg.DBPath = envDB
	}
	if envPort := os.Getenv("SERVER_PORT"); envPort != "" {
		cfg.ServerPort = envPort
	}
	if envRedis := os.Getenv("REDIS_ADDR"); envRedis != "" {
		cfg.RedisAddr = envRedis
	}
	if cfg.RedisAddr == "" {
		cfg.RedisAddr = "localhost:6379"
	}

	return &cfg
}

func (c *Config) DSN() string {
	return c.DBPath
}
