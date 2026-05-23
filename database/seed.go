package database

import (
	"fmt"
	"log"
)

func Seed() {

	wallets := []struct {
		id      string
		balance int64
	}{
		{id: "wallet_1", balance: 10000},
		{id: "wallet_2", balance: 60000},
	}

	for _, w := range wallets {
		_, err := DB.Exec(`
			INSERT INTO wallets (wallet_id, balance)
			VALUES ($1, $2)
			ON CONFLICT (wallet_id) DO NOTHING
		`, w.id, w.balance)
		if err != nil {
			log.Fatalf("Seed failed for wallet %s: %v", w.id, err)
		}
	}

	fmt.Println("Database seeding completed successfully...")
}
