package main

import (
	"database/sql"
	"time"
)

func migrate(db *sql.DB) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS wallets (
            id TEXT PRIMARY KEY,
            balance INTEGER NOT NULL DEFAULT 0,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        );`,
		`CREATE TABLE IF NOT EXISTS transfers (
            id TEXT PRIMARY KEY,
            from_wallet_id TEXT NOT NULL,
            to_wallet_id TEXT NOT NULL,
            amount INTEGER NOT NULL,
            state TEXT NOT NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(from_wallet_id) REFERENCES wallets(id),
            FOREIGN KEY(to_wallet_id) REFERENCES wallets(id)
        );`,
		`CREATE TABLE IF NOT EXISTS ledger_entries (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            wallet_id TEXT NOT NULL,
            transfer_id TEXT NOT NULL,
            type TEXT NOT NULL,
            amount INTEGER NOT NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY(wallet_id) REFERENCES wallets(id),
            FOREIGN KEY(transfer_id) REFERENCES transfers(id)
        );`,
		`CREATE TABLE IF NOT EXISTS idempotency_records (
            idempotency_key TEXT PRIMARY KEY,
            transfer_id TEXT,
            status TEXT NOT NULL,
            response TEXT,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
        );`,
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}

	// ensure some reasonable default timeout
	db.SetConnMaxLifetime(time.Minute * 5)
	return nil
}
