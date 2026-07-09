package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/handler"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/repository/sqlite"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/service"
)

func main() {
	dbPath := getenv("DB_PATH", "wallets.db")
	addr := getenv("HTTP_ADDR", ":8080")

	db, err := sql.Open("sqlite3", sqlite.DSN(dbPath))
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store := sqlite.NewStore(db)
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	if err := store.SeedWallets(ctx, map[string]int64{
		"wallet_1": 1000,
		"wallet_2": 1000,
		"wallet_3": 1000,
	}); err != nil {
		log.Fatalf("seed wallets: %v", err)
	}

	svc := service.NewTransferService(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, svc)

	log.Printf("wallet transfer service listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
