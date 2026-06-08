package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/handler"
	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
	"github.com/aks-95/wallet-transfer-assignment/internal/service"
)

func main() {
	addr := envOrDefault("SERVER_ADDR", ":8080")

	ctx := context.Background()
	db, err := repository.NewFromEnv(ctx)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	if err := migrate.Up(ctx, db.Pool); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	transferService := service.NewTransferService(db)
	router := handler.NewRouter(handler.Dependencies{
		TransferService: transferService,
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
