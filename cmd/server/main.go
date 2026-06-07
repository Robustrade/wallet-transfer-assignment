package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/Saket121/wallet-transfer-assignment/internal/handler"
	"github.com/Saket121/wallet-transfer-assignment/internal/middleware"
	"github.com/Saket121/wallet-transfer-assignment/internal/repository"
	"github.com/Saket121/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Database setup
	dbPath := getEnv("DATABASE_PATH", "wallet_transfer.db")
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Enable foreign keys and WAL mode
	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000"); err != nil {
		logger.Error("failed to set pragmas", "error", err)
		os.Exit(1)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	logger.Info("database ready", "path", dbPath)

	// Initialize layers
	repo := repository.NewSQLiteRepository(db)
	transferService := service.NewTransferService(repo, logger)
	walletService := service.NewWalletService(repo)
	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService)

	// Set up routes
	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("/wallets/", walletHandler.GetBalance)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Apply middleware
	loggingMiddleware := middleware.Logging(logger)
	server := &http.Server{
		Addr:         getEnv("PORT", ":8080"),
		Handler:      loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down server")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown error", "error", err)
		}
	}()

	logger.Info(fmt.Sprintf("server starting on %s", server.Addr))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped")
}

func runMigrations(db *sql.DB) error {
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	migrationFile := filepath.Join(projectRoot, "db", "migrations", "001_initial_schema.sql")

	migrationSQL, err := os.ReadFile(migrationFile)
	if err != nil {
		return fmt.Errorf("reading migration file: %w", err)
	}

	if _, err := db.Exec(string(migrationSQL)); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}
	return nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
