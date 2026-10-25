package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/repository"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	transporthttp "github.com/dale-tomson/wallet-transfer-assignment/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Database initialization
	dsn := os.Getenv("DATABASE_PATH")
	if dsn == "" {
		dsn = "wallet.db"
	}

	slog.Info("Initializing SQLite connection pool", "dsn", dsn)
	db, err := repository.NewSQLiteDB(dsn)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Layer dependency injection using existing repository constructors
	txManager := repository.NewSQLTxManager(db)
	walletRepo := repository.NewSQLWalletRepository(db)
	transferRepo := repository.NewSQLTransferRepository(db)
	idempotencyRepo := repository.NewSQLIdempotencyRepository(db)

	walletService := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)
	handler := transporthttp.NewWalletHandler(walletService)
	router := transporthttp.RegisterRoutes(handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown handling
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Wallet Transfer Service running", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server encountered fatal error", "error", err)
			os.Exit(1)
		}
	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Failed to stop server gracefully", "error", err)
			_ = server.Close()
		}
	}

	slog.Info("Server stopped cleanly")
}
