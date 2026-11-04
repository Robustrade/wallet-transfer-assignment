package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/database"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/handler"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/service"
	"github.com/abhiabhi0/wallet-transfer-assignment/migrations"
)

const (
	defaultHTTPAddress = ":8080"
	shutdownTimeout    = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		log.Printf("wallet transfer server stopped: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dbConfig, err := database.LoadConfig()
	if err != nil {
		return fmt.Errorf("load database configuration: %w", err)
	}

	db, err := database.Open(ctx, stdlib.GetDefaultDriver(), dbConfig)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	if err := migrations.Apply(ctx, db); err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	transferService := service.NewTransferService(service.SQLTransactionManager{DB: db})
	mux := http.NewServeMux()
	mux.Handle("/transfers", handler.NewTransferHandler(transferService))
	mux.HandleFunc("/healthz", healthHandler(db))

	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = defaultHTTPAddress
	}
	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		return nil
	}
}

func healthHandler(db interface {
	PingContext(context.Context) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\n"))
	}
}
