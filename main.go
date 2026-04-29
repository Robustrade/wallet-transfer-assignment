package main

import (
	"log"
	"wallet-transfer/app"
	"wallet-transfer/config"
)

func main() {
	// Load configuration
	cfg := config.LoadConfig()

	// Initialize application
	appInstance, err := app.InitializeApp(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer appInstance.Close()

	// Start server
	if err := appInstance.Run(cfg.Server.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
