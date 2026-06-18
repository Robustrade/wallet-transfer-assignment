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

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/handler"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/service"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func run() error {
	// Load .env if present; ignore error so real env vars still work in prod.
	_ = godotenv.Load()

	dsn := getenv("DATABASE_URL", "postgres://wallet:wallet@localhost:5433/wallet_db?sslmode=disable")
	addr := getenv("HTTP_ADDR", ":8090")

	ctx := context.Background()

	store, err := repository.NewPostgresStore(ctx, dsn)
	if err != nil {
		return err
	}
	defer store.Close()

	svc := service.NewTransferService(store)
	h := handler.NewTransferHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.CreateTransfer)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run server in background.
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Wait for interrupt or server failure.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case <-stop:
		log.Println("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
