package env

import (
	"cmp"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func Init(envPath string) {
	if err := godotenv.Load(envPath); err != nil {
		log.Fatalf("Error while loading .env file from %s. Error: %+v", envPath, err)
	}
	fmt.Println(".env loaded")
}

func GetString(key, fallback string) string {
	value := os.Getenv(key)
	keyValue := cmp.Or(value, fallback)
	return keyValue
}
