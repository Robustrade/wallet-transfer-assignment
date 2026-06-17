package main

import (
	"context"
	"log"
	"os"

	"github.com/example/wallet-transfer/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatal(err)
	}
	if err := migrate.Apply(ctx, pool); err != nil {
		log.Fatal(err)
	}
	log.Println("migrations applied")
}
