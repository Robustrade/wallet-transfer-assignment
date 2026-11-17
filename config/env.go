package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads the nearest .env file (walking up from the working directory).
// Existing process environment variables take precedence over .env values.
func LoadDotEnv() {
	if path := findDotEnv(); path != "" {
		_ = godotenv.Load(path)
		return
	}
	_ = godotenv.Load()
}

func findDotEnv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
