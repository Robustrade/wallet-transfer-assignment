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

	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	ctx := context.Background()

	dsn := getenv("DATABASE_DSN", "file:wallet.db?_busy_timeout=5000&_foreign_keys=on")
	store, err := repository.OpenSQLite(ctx, dsn)
	if err != nil {
		logger.Fatalf("open database: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Printf("close database: %v", err)
		}
	}()

	if err := store.Migrate(ctx); err != nil {
		logger.Fatalf("migrate database: %v", err)
	}

	transferService := service.NewTransferService(store)
	api := httpapi.NewServer(transferService, logger)
	server := &http.Server{
		Addr:              ":" + getenv("PORT", "8080"),
		Handler:           httpapi.LoggingMiddleware(api, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Printf("wallet transfer service listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatalf("serve http: %v", err)
		}
	}()

	<-shutdownCtx.Done()

	gracefulCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(gracefulCtx); err != nil {
		logger.Printf("shutdown server: %v", err)
	}
}

func getenv(key string, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	return value
}
