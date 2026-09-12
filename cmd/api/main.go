package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log.Println("Applying database migrations...")
	migrationPool, err := postgres.NewPool(ctx, cfg.MigrationDatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to migration database: %v", err)
	}
	if err := postgres.Migrate(ctx, migrationPool); err != nil {
		migrationPool.Close()
		log.Fatalf("Failed to apply migrations: %v", err)
	}
	migrationPool.Close()
	log.Println("Database migrations applied successfully.")

	if parsedURL, err := url.Parse(cfg.DatabaseURL); err == nil && parsedURL.Host != "" {
		dbName := strings.TrimPrefix(parsedURL.Path, "/")
		log.Printf("Connecting to PostgreSQL at host=%s db=%s ...", parsedURL.Host, dbName)
	} else {
		log.Println("Connecting to PostgreSQL database...")
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to runtime database: %v", err)
	}
	defer pool.Close()

	// Construct repositories
	repos := repository.Repositories{
		Wallets:     postgres.NewWalletRepository(pool),
		Transfers:   postgres.NewTransferRepository(pool),
		Ledger:      postgres.NewLedgerRepository(pool),
		Idempotency: postgres.NewIdempotencyRepository(pool),
	}
	txManager := postgres.NewTxManager(pool)

	// Derive transaction concurrency capacity from actual pool.Config().MaxConns
	actualMaxConns := int(pool.Config().MaxConns)
	headroom := int(cfg.HeartbeatHeadroom)
	if headroom >= actualMaxConns {
		headroom = actualMaxConns / 3
	}
	if headroom < 1 {
		headroom = 1
	}
	maxConcurrentTransfers := actualMaxConns - headroom
	if maxConcurrentTransfers < 1 {
		maxConcurrentTransfers = 1
	}

	transferService := service.NewTransferServiceWithCapacity(txManager, repos, maxConcurrentTransfers)

	// Provision dedicated heartbeat pool and wire dedicated heartbeat repository
	heartbeatPool, hbErr := postgres.NewHeartbeatPool(ctx, cfg.DatabaseURL)
	if hbErr == nil {
		defer heartbeatPool.Close()
		transferService.SetHeartbeatRepo(postgres.NewIdempotencyRepository(heartbeatPool))
		log.Println("Dedicated heartbeat connection pool initialized successfully.")
	} else {
		log.Printf("[WARN] Failed to create dedicated heartbeat pool: %v; falling back to primary pool headroom", hbErr)
	}

	walletService := service.NewWalletService(txManager, repos)

	// Construct handlers
	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService, cfg.AdminKey)

	// Construct router
	router := handler.NewRouter(handler.Config{
		TransferHandler: transferHandler,
		WalletHandler:   walletHandler,
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server run context
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Wallet Transfer Service listening on port %s", cfg.Port)
		serverErrors <- server.ListenAndServe()
	}()

	// Graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	case sig := <-shutdown:
		log.Printf("Received signal %v, initiating graceful shutdown...", sig)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("Server forced to shutdown: %v", err)
		}
		log.Println("Server gracefully stopped.")
	}
}
