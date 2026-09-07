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

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/config"
	"wallet-transfer-service/internal/database"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/routes"
	"wallet-transfer-service/internal/service"
)

func main() {
	// Structured JSON logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load environment config
	cfg := config.LoadConfig()

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL connection pool via centralized database package
	db, err := database.ConnectDB(cfg.DatabaseURL)
	if err != nil {
		slog.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Repositories
	walletRepo := repository.NewWalletRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	idempotencyRepo := repository.NewIdempotencyRepository(db)

	// Services
	walletService := service.NewWalletService(walletRepo, transferRepo)
	transferService := service.NewTransferService(transferRepo, idempotencyRepo, cfg)

	// Handlers & Gin Router
	walletHandler := handler.NewWalletHandler(walletService)
	transferHandler := handler.NewTransferHandler(transferService)

	ginEngine := routes.SetupRouter(walletHandler, transferHandler, cfg.AllowedOrigins)

	// Standard HTTP Server with timeouts wrapping Gin engine
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      ginEngine,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Run server in background goroutine
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server fatal error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server exited cleanly")
}
