package platform

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func NewDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:wallet.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite allows one writer at a time. A single connection keeps transaction
	// boundaries predictable for this small service.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply database schema: %w", err)
	}
	if err := migrateWalletColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateLedgerEntries(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrateWalletColumns(db *sql.DB) error {
	columns, err := tableColumns(db, "wallets")
	if err != nil {
		return err
	}
	if !columns["status"] {
		if _, err := db.Exec(`ALTER TABLE wallets ADD COLUMN status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE'))`); err != nil {
			return fmt.Errorf("add wallet status: %w", err)
		}
	}
	if !columns["updated_at"] {
		if _, err := db.Exec(`ALTER TABLE wallets ADD COLUMN updated_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add wallet updated_at: %w", err)
		}
		if _, err := db.Exec(`UPDATE wallets SET updated_at = CURRENT_TIMESTAMP WHERE updated_at = ''`); err != nil {
			return fmt.Errorf("backfill wallet updated_at: %w", err)
		}
	}
	return nil
}

func migrateLedgerEntries(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(ledger_entries)`)
	if err != nil {
		return fmt.Errorf("inspect ledger schema: %w", err)
	}
	transferIDNotNull := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("read ledger schema: %w", err)
		}
		if name == "transfer_id" {
			transferIDNotNull = notNull == 1
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate ledger schema: %w", err)
	}
	rows.Close()
	if !transferIDNotNull {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin ledger migration: %w", err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE ledger_entries_new (
			id TEXT PRIMARY KEY,
			wallet_id TEXT NOT NULL REFERENCES wallets(id),
			transfer_id TEXT REFERENCES transfers(id),
			type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
			amount INTEGER NOT NULL CHECK (amount > 0),
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (transfer_id, type)
		)`,
		`INSERT INTO ledger_entries_new (id, wallet_id, transfer_id, type, amount, created_at)
			SELECT id, wallet_id, transfer_id, type, amount, created_at FROM ledger_entries`,
		`DROP TABLE ledger_entries`,
		`ALTER TABLE ledger_entries_new RENAME TO ledger_entries`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("migrate ledger entries: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ledger migration: %w", err)
	}
	return nil
}

func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	// table is an internal constant, not user input.
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, fmt.Errorf("inspect %s schema: %w", table, err)
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, fmt.Errorf("read %s schema: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s schema: %w", table, err)
	}
	return columns, nil
}
