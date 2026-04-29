package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "wallet-transfer/docs"

	"wallet-transfer/internal/transfer/handler"
	transferRepo "wallet-transfer/internal/transfer/repository"
	transferService "wallet-transfer/internal/transfer/service"
	walletHandler "wallet-transfer/internal/wallet/handler"
	walletRepo "wallet-transfer/internal/wallet/repository"
	walletService "wallet-transfer/internal/wallet/service"
	"wallet-transfer/pkg/config"
	"wallet-transfer/pkg/database"
	"wallet-transfer/pkg/logger"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberSwagger "github.com/swaggo/fiber-swagger"
)

// @title Wallet Transfer API
// @version 1.0
// @description Wallet-to-wallet transfer service with idempotency, double-entry ledger, and concurrency safety.
// @host localhost:8080
// @BasePath /api/v1
// @schemes http
func main() {
	// ── Load config ────────────────────────────────────────────────────────────
	if err := config.Load(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// ── Init logger ────────────────────────────────────────────────────────────
	logger.Init(config.Instance.LogLevel)

	//Connect to database
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := database.Connect(ctx, config.Instance.DBURL); err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer database.Close()

	pool := database.GetPool()

	wRepo := walletRepo.NewWalletRepository()
	wSvc := walletService.NewWalletService(pool, wRepo)
	wHandler := walletHandler.NewWalletHandler(wSvc)

	tRepo := transferRepo.NewTransferRepository()
	tSvc := transferService.NewTransferService(pool, tRepo, wRepo)
	tHandler := handler.NewTransferHandler(tSvc)

	// ── Setup Fiber ────────────────────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Printf("unhandled error in request %s %s: %v", c.Method(), c.Path(), err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"code":    fiber.StatusInternalServerError,
				"message": "internal server error",
			})
		},
	})

	app.Use(recover.New())

	// ── Swagger ────────────────────────────────────────────────────────────────
	app.Get("/swagger/*", fiberSwagger.WrapHandler)

	// ── Health check ───────────────────────────────────────────────────────────
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "time": time.Now()})
	})

	// ── API routes ─────────────────────────────────────────────────────────────
	api := app.Group("/api/v1")
	walletHandler.RegisterRoutes(api, wHandler)
	handler.RegisterRoutes(api, tHandler)

	// ── Graceful shutdown ──────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		addr := config.Instance.BindingAddress
		logger.Info("main", fmt.Sprintf("server starting on %s", addr), nil)
		if err := app.Listen(addr); err != nil {
			logger.Error("main", "server error", err, nil)
		}
	}()

	<-quit
	logger.Info("main", "shutting down server", nil)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	if err := app.ShutdownWithContext(shutCtx); err != nil {
		logger.Error("main", "shutdown error", err, nil)
	}

	logger.Info("main", "server exited gracefully", nil)
}
