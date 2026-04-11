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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := os.Getenv("DATABASE_DSN")
	if dsn == "" {
		dsn = "file:wallet.db?_foreign_keys=on"
	}

	database, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	walletRepo := repository.NewWalletRepository(database)
	transferRepo := repository.NewTransferRepository(database)
	ledgerRepo := repository.NewLedgerRepository()

	transferSvc := service.NewTransferService(database, walletRepo, transferRepo, ledgerRepo)

	transferHandler := handler.NewTransferHandler(transferSvc)
	walletHandler := handler.NewWalletHandler(walletRepo)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Post("/wallets", walletHandler.CreateWallet)
	r.Get("/wallets/{id}", walletHandler.GetWallet)
	r.Post("/transfers", transferHandler.CreateTransfer)
	r.Get("/transfers/{id}", transferHandler.GetTransfer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		log.Printf("server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Block until interrupt or SIGTERM.
	<-ctx.Done()
	stop()
	log.Println("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("graceful shutdown failed: %v", err)
	}

	log.Println("server stopped")
}
