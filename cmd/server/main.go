package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/database"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/handler"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/repository"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/service"
)

func main() {
	//connect to psql database
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	//set context with timeout for database connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	//we send context to database connection so that it can be cancelled if it takes too long
	db, err := database.NewPostgres(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	dbRepo := repository.NewDBRepository()

	transferService := service.NewTransferService(
		db,
		dbRepo,
	)

	transferHandler := handler.NewTransferHandler(transferService)

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /transfers",
		transferHandler.CreateTransfer,
	)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Println("server started on :8080")

		if err := server.ListenAndServe(); err != nil &&
			err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	//graceful shutdown
	shutdownCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	<-shutdownCtx.Done()

	log.Println("shutting down server")

	//set context with timeout for server shutdown , so that if we have any long running requests, we can wait for them to finish before shutting down the server
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
}
