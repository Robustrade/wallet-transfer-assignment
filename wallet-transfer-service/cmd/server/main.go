package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/database"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/handler"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/repository"
	"github.com/Swapnil811/wallet-transfer-assignment/wallet-transfer-service/internal/service"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable"
	}

	pool, err := database.NewPool(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool, os.DirFS("migrations")); err != nil {
		log.Fatal(err)
	}

	repos := repository.NewRepositories()
	transferService := service.NewTransferService(pool, repos)
	transferHandler := handler.NewTransferHandler(transferService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("wallet transfer service listening on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
