package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/handler"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/repository"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Configuration from environment variables with sensible defaults
	dbURL := getEnv("DATABASE_URL", "postgres://wallet:wallet_secret@localhost:5432/wallet_transfer?sslmode=disable")
	port := getEnv("SERVER_PORT", "8080")

	ctx := context.Background()

	// ── Database ──────────────────────────────────────────────────────────
	logger.Info("connecting to database...")
	db, err := repository.NewDB(ctx, dbURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("database connected")

	// ── Migrations & Seeds ────────────────────────────────────────────────
	logger.Info("running migrations...")
	if err := db.RunMigrations(ctx, "migrations"); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("migrations completed")

	appEnv := getEnv("APP_ENV", "development")
	runSeeds := getEnv("RUN_SEEDS", "false")
	if appEnv != "production" && runSeeds == "true" {
		if _, err := os.Stat("seeds"); err == nil {
			logger.Info("running database seeds...")
			if err := db.RunMigrations(ctx, "seeds"); err != nil {
				logger.Error("failed to run database seeds", "error", err)
				os.Exit(1)
			}
			logger.Info("database seeds completed")
		} else {
			logger.Info("seeds directory not found, skipping seed execution")
		}
	}

	// ── Dependency Wiring ─────────────────────────────────────────────────
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempotencyRepo := repository.NewIdempotencyRepository()

	svc := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo, logger)

	transferHandler := handler.NewTransferHandler(svc, logger)
	walletHandler := handler.NewWalletHandler(svc, logger)

	// ── Router ────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middleware stack
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(handler.RequestLogger(logger))
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// Health check
	r.Get("/health", healthCheck)

	// Transfer routes
	r.Route("/transfers", func(r chi.Router) {
		r.Post("/", transferHandler.CreateTransfer)
		r.Get("/{id}", transferHandler.GetTransfer)
	})

	// Wallet routes
	r.Route("/wallets", func(r chi.Router) {
		r.Post("/", walletHandler.CreateWallet)
		r.Get("/{id}", walletHandler.GetWallet)
	})

	// ── HTTP Server ───────────────────────────────────────────────────────
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("server starting", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	logger.Info("server stopped gracefully")
}

// healthCheck returns a simple health status for load balancers and monitoring.
func healthCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy"}`)) //nolint:errcheck
}

// getEnv reads an environment variable with a fallback default.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
