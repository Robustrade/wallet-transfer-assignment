package main

import (
	"context"

	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shanks/wallet-transfer-assignment/internal/handler"
	"github.com/shanks/wallet-transfer-assignment/internal/repository"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
)

//go:embed schema.sql
var schemaSQL string

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://postgres:postgres@localhost:5432/wallet_db?sslmode=disable"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("connecting to PostgreSQL database", "url", dbURL)
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		logger.Error("failed to open database connection", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		logger.Error("failed to ping PostgreSQL database", "error", err)
		os.Exit(1)
	}

	logger.Info("applying database schema migrations")
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		logger.Error("failed to execute schema DDL", "error", err)
		os.Exit(1)
	}

	// Constructor Dependency Injection
	txMgr := repository.NewPostgresTxManager(db)
	walletRepo := repository.NewPostgresWalletRepository(db)
	transferRepo := repository.NewPostgresTransferRepository(db)
	ledgerRepo := repository.NewPostgresLedgerRepository(db)
	idempotencyRepo := repository.NewPostgresIdempotencyRepository(db)

	walletSvc := service.NewWalletService(txMgr, walletRepo, transferRepo, ledgerRepo)
	transferSvc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	walletHandler := handler.NewWalletHandler(walletSvc)
	transferHandler := handler.NewTransferHandler(transferSvc)

	router := handler.NewRouter(walletHandler, transferHandler)

	serverAddr := fmt.Sprintf(":%s", port)
	srv := &http.Server{
		Addr:         serverAddr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("starting wallet transfer HTTP server", "address", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-shutdown
	logger.Info("shutting down HTTP server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server graceful shutdown failed", "error", err)
	} else {
		logger.Info("HTTP server stopped gracefully")
	}
}
