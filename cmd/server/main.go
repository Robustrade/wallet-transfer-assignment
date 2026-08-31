package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/database"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"
)

func main() {

	ctx := context.Background()

	// ========================================================
	// 1. Database configuration
	// ========================================================

	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db := database.ConnectPostgres(
		ctx,
		databaseURL,
	)

	defer db.Close()

	// ========================================================
	// 2. Repository
	// ========================================================

	repo := repository.NewPostgresTransferRepository(
		db,
	)

	// ========================================================
	// 3. Service
	// ========================================================

	transferService := service.NewTransferService(
		repo,
	)

	// ========================================================
	// 4. Handler
	// ========================================================

	transferHandler := handler.NewTransferHandler(
		transferService,
	)

	// ========================================================
	// 5. Router
	// ========================================================

	router := gin.Default()

	router.POST(
		"/transfers",
		transferHandler.CreateTransfer,
	)

	// ========================================================
	// 6. Start server
	// ========================================================

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {

		log.Println("wallet transfer service running on :8080")

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}

	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	// Block until signal is received
	<-quit

	log.Println("Shutdown signal received")

	// Give active requests 10 seconds to finish
	ctx, cancel := context.WithTimeout(
		context.Background(),
		40*time.Second,
	)
	defer cancel()

	// Gracefully shut down the HTTP server
	if err := server.Shutdown(ctx); err != nil {
		log.Println("Server forced to shutdown:", err)
	}

	log.Println("Server stopped")

}
