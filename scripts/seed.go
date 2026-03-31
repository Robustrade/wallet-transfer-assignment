//go:build ignore

package main

import (
	"database/sql"
	"log"

	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	_ = godotenv.Load()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to open db connection: %v", err)
	}
	defer db.Close()

	// Seed two persistent test wallets
	_, _ = db.Exec("INSERT INTO wallets (id, balance) VALUES ('test_wallet_a', 1000) ON CONFLICT (id) DO UPDATE SET balance = 1000;")
	_, _ = db.Exec("INSERT INTO wallets (id, balance) VALUES ('test_wallet_b', 0) ON CONFLICT (id) DO NOTHING;")

	log.Println("Seeded Database with 'test_wallet_a' ($1000) and 'test_wallet_b' ($0)!")
}
