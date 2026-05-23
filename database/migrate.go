package database

import (
	"fmt"
	"log"
)

func Migrate() {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS wallets (
			wallet_id   TEXT PRIMARY KEY,
			balance     BIGINT NOT NULL DEFAULT 0,
			status      TEXT NOT NULL DEFAULT 'ACTIVE',
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS transfers (
			transaction_id  TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			from_wallet_id  TEXT NOT NULL REFERENCES wallets(wallet_id),
			to_wallet_id    TEXT NOT NULL REFERENCES wallets(wallet_id),
			amount          BIGINT NOT NULL,
			status          TEXT NOT NULL DEFAULT 'PROCESSED',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		`CREATE TABLE IF NOT EXISTS ledger_entries (
			entry_id        TEXT PRIMARY KEY,
			transaction_id  TEXT NOT NULL REFERENCES transfers(transaction_id),
			wallet_id       TEXT NOT NULL REFERENCES wallets(wallet_id),
			amount          BIGINT NOT NULL,
			entry           TEXT NOT NULL,
			balance_before  BIGINT NOT NULL,
			balance_after   BIGINT NOT NULL,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, q := range queries {
		if _, err := DB.Exec(q); err != nil {
			log.Fatalf("Migration failed: %v", err)
		}
	}

	fmt.Println("Database migration completed successfully...")
}
