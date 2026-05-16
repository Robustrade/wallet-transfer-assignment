package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer/internal/database"
	"wallet-transfer/internal/idempotency"
	ledgerrepo "wallet-transfer/internal/ledger/repository"
	transferhandler "wallet-transfer/internal/transfer/handler"
	transferrepo "wallet-transfer/internal/transfer/repository"
	transferservice "wallet-transfer/internal/transfer/service"
	wallethandler "wallet-transfer/internal/wallet/handler"
	walletrepo "wallet-transfer/internal/wallet/repository"
	walletservice "wallet-transfer/internal/wallet/service"
	"wallet-transfer/pkg/logger"

	"github.com/gorilla/mux"
)

func main() {
	// Setup logger
	log := logger.New(logger.INFO)

	// Database configuration
	dbConfig := database.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     5432,
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres"),
		DBName:   getEnv("DB_NAME", "testdb"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	// Connect to database
	db, err := database.New(dbConfig)
	if err != nil {
		log.Error("failed to connect to database", map[string]interface{}{
			"error": err.Error(),
		})
		os.Exit(1)
	}
	defer db.Close()

	log.Info("connected to database", map[string]interface{}{
		"host": dbConfig.Host,
		"db":   dbConfig.DBName,
	})

	// Initialize repositories
	walletRepo := walletrepo.NewWalletRepository(db.GetDB())
	transferRepo := transferrepo.NewTransferRepository(db.GetDB())
	ledgerRepo := ledgerrepo.NewLedgerRepository(db.GetDB())
	idempotencyRepo := idempotency.NewIdempotencyRepository(db.GetDB())

	// Initialize services
	transferSvc := transferservice.NewTransferService(
		db,
		walletRepo,
		transferRepo,
		ledgerRepo,
		idempotencyRepo,
		log,
	)
	walletSvc := walletservice.NewWalletService(db, walletRepo, log)

	// Initialize handlers
	transferH := transferhandler.NewTransferHandler(transferSvc, log)
	walletH := wallethandler.NewWalletHandler(walletSvc, log)

	// Setup router
	router := setupRouter(transferH, walletH)

	// Start server
	addr := getEnv("SERVER_ADDR", ":8080")
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Info("starting server", map[string]interface{}{
			"address": addr,
		})
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("shutting down server", map[string]interface{}{})

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("server shutdown error", map[string]interface{}{
			"error": err.Error(),
		})
	}

	log.Info("server stopped", map[string]interface{}{})
}

func setupRouter(transferH *transferhandler.TransferHandler, walletH *wallethandler.WalletHandler) http.Handler {
	r := mux.NewRouter()

	// Transfer endpoints
	r.HandleFunc("/transfers", transferH.CreateTransfer).Methods(http.MethodPost)
	r.HandleFunc("/transfers/{transferId}", transferH.GetTransfer).Methods(http.MethodGet)

	// Wallet endpoints
	r.HandleFunc("/wallets/{walletId}", walletH.GetWallet).Methods(http.MethodGet)
	r.HandleFunc("/wallets/{walletId}/balance", walletH.GetBalance).Methods(http.MethodGet)

	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok"}`)
	}).Methods(http.MethodGet)

	return r
}

func getEnv(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
