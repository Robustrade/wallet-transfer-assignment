package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer/internal/app"
	"wallet-transfer/internal/repository"

	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Configuration
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		// dsn = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
		dsn = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
	}

	// 2. Initialize Database Connection
	db, err := repository.InitDB(dsn)
	if err != nil {
		// CRITICAL: We must Fatalf here. If the DB fails to connect, 'db' is nil.
		// Calling defer db.Close() later will cause a nil pointer panic.
		// A backend service should always "fail fast" if its core database is down.
		log.Fatalf("Failed to connect to database: %v", err)
		// log.Printf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// 1. Build all dependencies
	myApp := app.Initialize(db)

	// 4. Initialize Router
	r := gin.Default()
	myApp.SetupRouter(r)

	// 5. Configure HTTP Server
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// 6. Start Server in a Goroutine
	go func() {
		log.Println("Starting server on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	// 7. Graceful Shutdown Setup
	// Create a channel to listen for OS signals
	quit := make(chan os.Signal, 1)
	// Catch SIGINT (ctrl+c) and SIGTERM (docker container stop)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Block until a signal is received
	<-quit
	log.Println("Shutting down server gracefully...")

	// Give the server 5 seconds to finish in-flight requests (like DB transactions)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
}
