// Command seeddev inserts development wallet fixtures into the database named
// by DATABASE_URL. It is idempotent (ON CONFLICT DO NOTHING) and is meant for
// local use only — never run it against a real environment.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
)

type wallet struct {
	id       string
	balance  int64
	currency string
}

var fixtures = []wallet{
	{"wallet_1", 100_000, "USD"},
	{"wallet_2", 0, "USD"},
	{"wallet_3", 50_000, "USD"},
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "seeddev: DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seeddev: connect: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	for _, w := range fixtures {
		if _, err := conn.Exec(ctx,
			`INSERT INTO wallets (id, balance, currency) VALUES ($1, $2, $3)
			 ON CONFLICT (id) DO NOTHING`,
			w.id, w.balance, w.currency); err != nil {
			fmt.Fprintf(os.Stderr, "seeddev: insert %s: %v\n", w.id, err)
			os.Exit(1)
		}
		fmt.Printf("seeded %s (%d %s)\n", w.id, w.balance, w.currency)
	}
}
