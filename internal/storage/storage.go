package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func NewSQLiteDB(dataSource string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dataSource)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error {
	schema := `PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    balance INTEGER NOT NULL CHECK(balance >= 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transfers (
    id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL,
    to_wallet_id TEXT NOT NULL,
    amount INTEGER NOT NULL CHECK(amount > 0),
    state TEXT NOT NULL CHECK(state IN ('PENDING','PROCESSED','FAILED')),
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(from_wallet_id) REFERENCES wallets(id),
    FOREIGN KEY(to_wallet_id) REFERENCES wallets(id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    wallet_id TEXT NOT NULL,
    transfer_id TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('DEBIT','CREDIT')),
    amount INTEGER NOT NULL CHECK(amount > 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(wallet_id) REFERENCES wallets(id),
    FOREIGN KEY(transfer_id) REFERENCES transfers(id)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers(idempotency_key);
`
	_, err := db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	return nil
}
