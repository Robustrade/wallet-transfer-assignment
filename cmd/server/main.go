package main

import (
	"log"
	"time"

	"wallet-transfer-assignment/internal/api/routes"
	idempotencyController "wallet-transfer-assignment/internal/controller/idempotency"
	transferController "wallet-transfer-assignment/internal/controller/transfer"
	idempotencyHandlers "wallet-transfer-assignment/internal/handlers/idempotency"
	transferHandlers "wallet-transfer-assignment/internal/handlers/transfer"
	"wallet-transfer-assignment/internal/repositories"
	"wallet-transfer-assignment/internal/services/cache"
	"wallet-transfer-assignment/internal/services/database"
)

func main() {
	// 1. Establish the PostgreSQL connection and run migrations.
	database.InitDB()

	// 2. Initialize the In-Memory Idempotency Cache with a 24-hour TTL.
	// In production, this would be replaced by a Redis implementation.
	cacheService := cache.NewSyncMapCache(24 * time.Hour)

	// 3. Initialize the Persistence Layer.
	transferRepo := repositories.NewTransferRepository(database.DB)

	// 4. Initialize the Business Logic Layer (Controllers).
	idempotencyBiz := idempotencyController.NewIdempotencyControllerImpl(cacheService)
	transferBiz := transferController.NewTransferControllerImpl(transferRepo, cacheService)

	// 5. Initialize the API Layer (Handlers).
	transferHandler := transferHandlers.NewTransferHandler(transferBiz)
	idempotencyHandler := idempotencyHandlers.NewIdempotencyHandler(idempotencyBiz)

	// 6. Configure the Gin router with all routes and middlewares.
	r := routes.SetupRouter(transferHandler, idempotencyHandler, cacheService)

	// 7. Start the HTTP server.
	log.Println("Wallet Transfer Service starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
