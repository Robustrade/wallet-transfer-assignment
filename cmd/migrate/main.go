package main

import (
	"context"
	"log"
	"os"

	"github.com/aks-95/wallet-transfer-assignment/internal/migrate"
	"github.com/aks-95/wallet-transfer-assignment/internal/repository"
)

func main() {
	ctx := context.Background()

	db, err := repository.NewFromEnv(ctx)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	if err := migrate.Up(ctx, db.Pool); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	log.Println("migrations applied successfully")
	os.Exit(0)
}
