package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
	"wallet-transfer/repository"
	"wallet-transfer/service"
)

func initDB(db *sql.DB) {
	query := `
	CREATE TABLE IF NOT EXISTS wallets (
		id TEXT PRIMARY KEY,
		balance INTEGER NOT NULL
	);
	CREATE TABLE IF NOT EXISTS transfers (
		id TEXT PRIMARY KEY,
		idempotency_key TEXT UNIQUE NOT NULL,
		from_wallet_id TEXT NOT NULL,
		to_wallet_id TEXT NOT NULL,
		amount INTEGER NOT NULL,
		status TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS ledger_entries (
		id TEXT PRIMARY KEY,
		transfer_id TEXT NOT NULL,
		wallet_id TEXT NOT NULL,
		type TEXT NOT NULL,
		amount INTEGER NOT NULL
	);`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}
}

func main() {
	db, err := sql.Open("sqlite3", "./dev.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	initDB(db)

	repo := repository.NewWalletRepository(db)
	transferSvc := service.NewTransferService(repo)

	http.HandleFunc("/transfers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req service.TransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		resp, err := transferSvc.ProcessTransfer(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	log.Println("Server running on port 3000...")
	log.Fatal(http.ListenAndServe(":3000", nil))
}