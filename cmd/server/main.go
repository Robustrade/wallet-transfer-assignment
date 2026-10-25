package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/handler"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/memory"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
	}

	var repo repository.Repository
	var sqlDB *sql.DB

	useMemory := os.Getenv("USE_MEMORY_DB") == "true"
	if !useMemory {
		logger.Info("connecting to postgresql database", slog.String("url", dbURL))
		var err error
		sqlDB, err = sql.Open("postgres", dbURL)
		if err == nil {
			sqlDB.SetMaxOpenConns(25)
			sqlDB.SetMaxIdleConns(10)
			sqlDB.SetConnMaxLifetime(5 * time.Minute)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = sqlDB.PingContext(ctx)
			cancel()
		}

		if err != nil {
			logger.Warn("could not connect to postgresql, falling back to in-memory repository", slog.Any("error", err))
			repo = memory.NewMemoryRepository()
			sqlDB = nil
		} else {
			logger.Info("successfully connected to postgresql")
			repo = postgres.NewPostgresRepository(sqlDB)
		}
	} else {
		logger.Info("using in-memory repository as configured")
		repo = memory.NewMemoryRepository()
	}
	defer repo.Close()

	// Initialize Services
	txService := service.NewTransferService(repo)
	walletService := service.NewWalletService(repo)

	// Initialize Handlers
	txHandler := handler.NewTransferHandler(txService)
	walletHandler := handler.NewWalletHandler(walletService)
	healthHandler := handler.NewHealthHandler(sqlDB)

	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", healthHandler.HealthCheck)

	// Interactive UI Dashboard
	mux.HandleFunc("GET /ui", handler.UIHandler)
	mux.HandleFunc("GET /", handler.UIHandler)

	// Transfers endpoints
	mux.HandleFunc("POST /transfers", txHandler.CreateTransfer)
	mux.HandleFunc("GET /transfers/", txHandler.GetTransfer)

	// Wallets endpoints
	mux.HandleFunc("POST /wallets", walletHandler.CreateWallet)
	mux.HandleFunc("GET /wallets/", walletHandler.GetWallet)

	// Apply Middlewares
	appHandler := handler.CORSMiddleware(
		handler.RecoveryMiddleware(logger)(
			handler.RequestLogger(logger)(mux),
		),
	)

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      appHandler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown setup
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("wallet transfer service starting", slog.String("port", port))
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("server error during startup", slog.Any("error", err))
		os.Exit(1)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", slog.Any("error", err))
			_ = server.Close()
		}
		logger.Info("server exited cleanly")
	}
}
