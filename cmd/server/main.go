package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/parth/wallet-transfer-assignment/internal/handlers"
	"github.com/parth/wallet-transfer-assignment/internal/services"
	"github.com/parth/wallet-transfer-assignment/pkg/db"
)

func main() {
	// Load .env file for local development
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found or error loading .env file:", err)
	}
	// Test DB connection
	if err := db.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	log.Println("Database connection successful!")

	// Run migrations
	databaseURL := os.Getenv("DATABASE_URL")
	if err := db.RunMigrations(databaseURL); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Initialize services
	transferService := services.NewTransferService(db.DB)

	// Initialize Gin router
	router := gin.Default()

	// Register routes
	handlers.RegisterRoutes(router, transferService)

	// Start HTTP server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
