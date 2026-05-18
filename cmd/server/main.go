package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/wallet-transfer-assignment/internal/config"
	"github.com/wallet-transfer-assignment/internal/database"
	"github.com/wallet-transfer-assignment/internal/handler"
	"github.com/wallet-transfer-assignment/internal/middleware"
	"github.com/wallet-transfer-assignment/internal/repository"
	"github.com/wallet-transfer-assignment/internal/service"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize logger
	logger := initLogger(cfg)

	logger.Info("server.starting",
		"db_driver", cfg.DBDriver,
		"port", cfg.Port,
	)

	// Create database connection
	db, err := database.NewDB(cfg)
	if err != nil {
		logger.Error("database.connection_failed", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	if err := database.RunMigrations(db, cfg.DBDriver); err != nil {
		logger.Error("database.migrations_failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("database.migrations_completed")

	// Seed test wallets
	if err := seedWallets(db, logger); err != nil {
		logger.Error("database.seed_failed", "error", err.Error())
		os.Exit(1)
	}

	// Create repository + tx manager based on driver
	var txManager repository.TxManager
	switch cfg.DBDriver {
	case "sqlite":
		txManager = repository.NewSQLiteRepository(db)
	case "postgres":
		txManager = repository.NewPostgresRepository(db)
	default:
		logger.Error("unsupported database driver", "driver", cfg.DBDriver)
		os.Exit(1)
	}

	// Wire layers
	svc := service.NewTransferService(txManager, logger)
	h := handler.NewTransferHandler(svc, logger)

	// Set up routes with middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", h.HandleCreateTransfer)

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Apply middleware chain: Recovery → RequestID → Logging → Handler
	var chain http.Handler = mux
	chain = middleware.Logging(logger)(chain)
	chain = middleware.RequestID(chain)
	chain = middleware.Recovery(logger)(chain)

	// Start server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      chain,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		logger.Info("server.shutting_down", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			logger.Error("server.shutdown_error", "error", err.Error())
		}
	}()

	logger.Info("server.listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("server.listen_error", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("server.stopped")
}

// initLogger creates a structured logger based on configuration.
func initLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(h)
}

// seedWallets creates test wallets if they don't already exist.
func seedWallets(db *sql.DB, logger *slog.Logger) error {
	wallets := []struct {
		id      string
		balance int64
	}{
		{"wallet_1", 10000},
		{"wallet_2", 10000},
		{"wallet_3", 5000},
	}

	for _, w := range wallets {
		result, err := db.Exec(
			"INSERT OR IGNORE INTO wallets (id, balance) VALUES (?, ?)",
			w.id, w.balance,
		)
		if err != nil {
			// Try PostgreSQL syntax if SQLite fails
			result, err = db.Exec(
				"INSERT INTO wallets (id, balance) VALUES ($1, $2) ON CONFLICT (id) DO NOTHING",
				w.id, w.balance,
			)
			if err != nil {
				return fmt.Errorf("seed wallet %s: %w", w.id, err)
			}
		}
		rows, _ := result.RowsAffected()
		if rows > 0 {
			logger.Info("database.wallet_seeded", "wallet_id", w.id, "balance", w.balance)
		}
	}
	return nil
}
