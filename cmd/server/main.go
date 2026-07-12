package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/api"
	"github.com/Robustrade/wallet-transfer-assignment/internal/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/logging"
	pgrepo "github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	logger := logging.NewLogger(cfg.LogLevel, nil)
	logger.Info("service configuration loaded listenAddr=%s", cfg.ListenAddr)

	gormDB, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		logger.Error("open database: %v", err)
		log.Fatalf("open database: %v", err)
	}

	rawDB, err := gormDB.DB()
	if err != nil {
		logger.Error("database driver setup: %v", err)
		log.Fatalf("database driver setup: %v", err)
	}
	defer rawDB.Close()

	rawDB.SetMaxOpenConns(cfg.DBMaxOpenConns)
	rawDB.SetMaxIdleConns(cfg.DBMaxIdleConns)

	walletRepo := pgrepo.NewWalletRepository(gormDB)
	transferRepo := pgrepo.NewTransferRepository(gormDB)
	ledgerRepo := pgrepo.NewLedgerRepository(gormDB)
	idempotencyRepo := pgrepo.NewIdempotencyRepository(gormDB)
	transferSvc := service.NewTransferService(gormDB, walletRepo, transferRepo, ledgerRepo, idempotencyRepo, logger)
	handler := api.NewTransferHandler(transferSvc)

	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("starting wallet transfer service addr=%s", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error: %v", err)
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed: %v", err)
		if closeErr := server.Close(); closeErr != nil {
			logger.Error("forced close failed: %v", closeErr)
		}
	}
	logger.Info("service stopped")
	_ = os.Stdout.Sync()
}
