package main

import (
	"context"
	"log"

	"wallet-transfer/internal/app"
	"wallet-transfer/internal/platform/config"
)

func main() {
	application, err := app.New(context.Background(), config.Load())
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(application.Run())
}
