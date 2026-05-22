package environment

import (
	"os"

	"github.com/joho/godotenv"
)

var envs map[string]string

// GoDotEnvVariable to load/read the .env file and return the value of the key
func GoDotEnvVariable(key, fallback string) string {
	if envs == nil {
		// Try to load .env from the config directory relative to the current working directory
		err := godotenv.Load("config/.env")
		if err != nil {
			// Fallback to checking if it exists in the current directory or parent
			_ = godotenv.Load()
		}

		// Map variables from environment since godotenv.Load sets them in the process
		// But the current implementation uses a local map. Let's stay consistent with the project's logic
		// but make it not fatal.
		envs, _ = godotenv.Read("config/.env")
		if envs == nil {
			envs = make(map[string]string)
		}
	}

	val := envs[key]
	if val == "" {
		val = os.Getenv(key)
	}

	if val != "" {
		return val
	}

	return fallback
}
