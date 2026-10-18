package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer-assignment/internal/bootstrap"
	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/feature/wallet/route"
	"wallet-transfer-assignment/internal/middleware"

	"github.com/gofiber/fiber/v3"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// run returns instead of calling os.Exit so deferred cleanup (DB close,
	// signal handler release) always executes.
	if err := run(logger); err != nil {
		logger.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	logger.Info(
		"configuration loaded successfully",
		"environment", cfg.AppEnv,
		"port", cfg.AppPort,
	)

	// Connect PostgreSQL
	db, err := database.Connect(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get database connection: %w", err)
	}
	defer sqlDB.Close()
	logger.Info("database connected successfully")

	app := fiber.New(fiber.Config{
		AppName:      "Wallet Transfer Service",
		ErrorHandler: middleware.ErrorHandler,
	})

	app.Get("/health", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"service": "wallet-transfer-service",
		})
	})

	// Register global middleware
	middleware.Register(app)

	// Initialize dependencies
	application := bootstrap.NewApp(db)

	// Register routes
	route.RegisterRoutes(app, route.Handlers{
		Transfer: application.TransferHandler,
	})

	serverErr := make(chan error, 1)

	go func() {
		address := fmt.Sprintf(":%s", cfg.AppPort)
		logger.Info("starting HTTP server", "address", address)
		if err := app.Listen(address); err != nil {
			serverErr <- err
		}
	}()

	signalCtx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	select {
	case <-signalCtx.Done():
		logger.Info("shutdown signal received")

	case err := <-serverErr:
		return fmt.Errorf("HTTP server stopped unexpectedly: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	logger.Info("application stopped")
	return nil
}
