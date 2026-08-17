package main

import (
	"errors"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/raknay/wallet-transfer-assignment/app"
)

// import "github.com/raknay/wallet-transfer-assignment/db"

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("failed to load env vars: %v", err)
	}
	app.Run()
}
