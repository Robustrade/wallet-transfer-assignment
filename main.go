package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/robustrade/wallet-transfer-assignment/internal/app"
	"github.com/robustrade/wallet-transfer-assignment/internal/storage"
)

func main() {
	ctx := context.Background()

	dbPath := os.Getenv("DATABASE_URL")
	if dbPath == "" {
		dbPath = "file:wallets.db?_foreign_keys=1&cache=shared&journal_mode=WAL&busy_timeout=5000"
	}

	db, err := storage.NewSQLiteDB(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := storage.Migrate(ctx, db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	repo := app.NewRepository(db)
	service := app.NewService(repo)
	server := app.NewServer(service)

	addr := ":8080"
	log.Printf("wallet transfer service listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Router()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
