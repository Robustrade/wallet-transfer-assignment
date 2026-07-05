// Command server wires up and runs the wallet transfer HTTP API.
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/example/wallet-transfer/internal/handler"
	"github.com/example/wallet-transfer/internal/repository/postgres"
	"github.com/example/wallet-transfer/internal/service"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required, e.g. postgres://user:pass@localhost:5432/wallets?sslmode=disable")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	store := postgres.NewStore(db)
	svc := service.NewTransferService(store)
	h := handler.NewTransferHandler(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", h.CreateTransfer) // Go 1.22+ method-aware routing

	addr := ":8080"
	log.Printf("wallet-transfer listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
