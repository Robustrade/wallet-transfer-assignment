package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/db"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/handler"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/service"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	ctx := context.Background()
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, relying on real environment variables")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable is required (set it in .env or your shell)")
	}

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	txManager := repository.NewTxManager(pool)
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempRepo := repository.NewIdempotencyRepository()

	transferService := service.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo, idempRepo)
	transferHandler := handler.NewTransferHandler(transferService)

	e := echo.New()
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())

	e.POST("/transfers", transferHandler.CreateTransfer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	go func() {
		log.Printf("listening on :%s", port)
		if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Wait for SIGINT or SIGTERM (eg `docker stop`, k8s pod eviction).
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("shutdown signal received, draining in-flight requests...")

	// Give in-flight requests (e.g. a transfer mid-transaction) up to 10s to
	// finish before forcing shutdown. This matters here specifically: we
	// don't want to kill the process between acquiring wallet locks and
	// committing the transaction.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("forced shutdown: %v", err)
	}
	log.Println("server exited cleanly")

}
