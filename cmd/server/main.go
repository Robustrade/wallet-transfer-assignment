package main

import (
	"log"

	"github.com/labstack/echo/v4"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/config"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/db"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/handler"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/router"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	conn, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connection error: %v", err)
	}
	defer conn.Close()

	if err := db.RunMigrations(conn, "migrations"); err != nil {
		log.Fatalf("migration error: %v", err)
	}

	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	transferService := service.NewTransferService(conn, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	transferHandler := handler.NewTransferHandler(transferService)

	walletQueryService := service.NewWalletQueryService(conn, walletRepo, transferRepo)
	walletHandler := handler.NewWalletHandler(walletQueryService)

	e := echo.New()
	router.Register(e, transferHandler, walletHandler)

	log.Printf("starting server on port %s", cfg.Port)
	if err := e.Start(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
