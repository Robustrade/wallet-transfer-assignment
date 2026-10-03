package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting %s in %s mode...\n", cfg.ServiceName, cfg.Environment)

	// 2. Initialize Database Connection & Auto-Migrate
	db, err := database.InitDB(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database initialization error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Connected to database successfully.")

	// 3. Initialize Repository and Services
	repo := postgres.NewRepository(db)
	walletSvc := service.NewWalletService(repo)
	transferSvc := service.NewTransferService(repo)

	// 4. Initialize Handlers
	walletHandler := handler.NewWalletHandler(walletSvc)
	transferHandler := handler.NewTransferHandler(transferSvc)
	healthHandler := handler.NewHealthHandler(cfg.ServiceName)

	// 5. Setup Echo Server
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())

	// Route Registration
	e.GET("/health", healthHandler.Check)

	// Transfer Routes
	e.POST("/transfers", transferHandler.CreateTransfer)
	e.GET("/transfers/:id", transferHandler.GetTransfer)
	e.GET("/transfers/:id/ledger", transferHandler.GetTransferLedger)

	// Wallet Routes
	e.POST("/wallets", walletHandler.CreateWallet)
	e.GET("/wallets/:id", walletHandler.GetWallet)
	e.GET("/wallets/:id/balance", walletHandler.GetBalance)

	// 6. Start HTTP Server with Graceful Shutdown
	address := ":" + cfg.Port
	go func() {
		if err := e.Start(address); err != nil && err != http.ErrServerClosed {
			e.Logger.Fatalf("Server shutdown error: %v", err)
		}
	}()
	fmt.Printf("HTTP server listening on %s\n", address)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	fmt.Println("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatal(err)
	}
	fmt.Println("Server gracefully stopped.")
}
