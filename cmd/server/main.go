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

	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/config"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/database"
	db "github.com/mritunjaysingh22/wallet-transfer-assignment/internal/db"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/handler"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/routes"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func main() {

	//----------------------------------------------------
	// Load Configuration
	//----------------------------------------------------

	cfg := config.Load()

	//----------------------------------------------------
	// PostgreSQL
	//----------------------------------------------------

	pool, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("unable to connect database: %v", err)
	}
	defer pool.Close()

	log.Println("Connected to PostgreSQL")

	//----------------------------------------------------
	// sqlc
	//----------------------------------------------------

	queries := db.New(pool)

	//----------------------------------------------------
	// Services
	//----------------------------------------------------

	transferService := service.NewTransferService(
		pool,
		queries,
	)

	//----------------------------------------------------
	// Handlers
	//----------------------------------------------------

	transferHandler := handler.NewTransferHandler(
		transferService,
	)

	//----------------------------------------------------
	// Router
	//----------------------------------------------------

	router := routes.NewRouter(
		transferHandler,
	)

	//----------------------------------------------------
	// HTTP Server
	//----------------------------------------------------

	server := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	//----------------------------------------------------
	// Start Server
	//----------------------------------------------------

	go func() {

		log.Printf("Server started on :%s", cfg.ServerPort)

		if err := server.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {

			log.Fatalf("server failed: %v", err)
		}
	}()

	//----------------------------------------------------
	// Graceful Shutdown
	//----------------------------------------------------

	stop := make(chan os.Signal, 1)

	signal.Notify(
		stop,
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	<-stop

	log.Println("Shutdown signal received...")

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}

	log.Println("Server stopped gracefully")
}
