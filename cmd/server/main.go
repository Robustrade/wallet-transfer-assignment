package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/example/wallet-transfer-assignment/internal/config"
	"github.com/example/wallet-transfer-assignment/internal/db"
	"github.com/example/wallet-transfer-assignment/internal/handler"
	"github.com/example/wallet-transfer-assignment/internal/repository"
	"github.com/example/wallet-transfer-assignment/internal/service"
)

func main() {
	cfg := config.Load()

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	walletRepo := repository.NewWalletRepo(database)
	transferRepo := repository.NewTransferRepo(database)
	idempotencyRepo := repository.NewIdempotencyRepo(database)
	transferService := service.NewTransferService(database, walletRepo, transferRepo, idempotencyRepo)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(transferService)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler.SetupRoutes(r, transferHandler, walletHandler)

	fmt.Printf("Server starting on %s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(cfg.Port, r))
}
