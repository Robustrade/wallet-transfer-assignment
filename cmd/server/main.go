package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"wallet-service/internal/handler"
	"wallet-service/internal/repository"
	"wallet-service/internal/repository/postgres"
	"wallet-service/internal/repository/sqlite"
	"wallet-service/internal/service"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbType := os.Getenv("DB_TYPE")
	if dbType == "" {
		dbType = "sqlite" // default to sqlite
	}

	var db *sql.DB
	var err error
	var repo repository.Repository

	if dbType == "postgres" {
		dbConnStr := os.Getenv("POSTGRES_CONN_STR")
		if dbConnStr == "" {
			dbConnStr = "user=postgres password=postgres dbname=wallet sslmode=disable"
		}
		db, err = sql.Open("postgres", dbConnStr)
		if err != nil {
			log.Fatalf("failed to connect to postgres: %v", err)
		}
		repo = postgres.NewPostgresRepository(db)
	} else {
		db, err = sql.Open("sqlite3", "file:wallet.db?cache=shared&mode=rwc")
		if err != nil {
			log.Fatalf("failed to connect to sqlite: %v", err)
		}
		// PRAGMA statements to enable foreign keys and busy timeout could be added here
		repo = sqlite.NewSQLiteRepository(db)
	}

	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping db: %v", err)
	}
	defer db.Close()

	initDB(db)

	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService, repo)

	mux := http.NewServeMux()
	mux.HandleFunc("/transfers", transferHandler.HandleTransfer) // Will only accept POST logically if checked, simplified here

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func initDB(db *sql.DB) {
	// NOTE : These queries are duplicated from schema.sql to ensure the
	// application runs out-of-the-box for demonstration and local testing without
	// manual database setup. In a real production environment, this function should
	// be removed and replaced with a proper migration tool (e.g., golang-migrate)
	// or CI/CD pipeline executing the standalone schema.sql file.

	createWalletsTable := `
	CREATE TABLE IF NOT EXISTS wallets (
		id VARCHAR(255) PRIMARY KEY,
		balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);`

	createTransfersTable := `
	CREATE TABLE IF NOT EXISTS transfers (
		id VARCHAR(255) PRIMARY KEY,
		idempotency_key VARCHAR(255) NOT NULL,
		from_wallet_id VARCHAR(255) NOT NULL,
		to_wallet_id VARCHAR(255) NOT NULL,
		amount BIGINT NOT NULL,
		state VARCHAR(50) NOT NULL,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	);`

	createLedgerEntriesTable := `
	CREATE TABLE IF NOT EXISTS ledger_entries (
		id VARCHAR(255) PRIMARY KEY,
		wallet_id VARCHAR(255) NOT NULL,
		transfer_id VARCHAR(255) NOT NULL,
		type VARCHAR(50) NOT NULL,
		amount BIGINT NOT NULL,
		created_at TIMESTAMP NOT NULL
	);`

	createIdempotencyRecordsTable := `
	CREATE TABLE IF NOT EXISTS idempotency_records (
		key VARCHAR(255) PRIMARY KEY,
		response TEXT NOT NULL,
		status_code INTEGER NOT NULL,
		created_at TIMESTAMP NOT NULL
	);`

	queries := []string{createWalletsTable, createTransfersTable, createLedgerEntriesTable, createIdempotencyRecordsTable}

	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			log.Fatalf("failed to initialize db schema: %v", err)
		}
	}

	// Seed some initial data for testing if not exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM wallets").Scan(&count)
	if count == 0 {
		_, err := db.Exec("INSERT INTO wallets (id, balance, created_at, updated_at) VALUES ('wallet_1', 1000, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), ('wallet_2', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
		if err != nil {
			fmt.Printf("Seed error (might be expected): %v\n", err)
		}
	}
}
