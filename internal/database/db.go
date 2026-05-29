
package database

import (
    "fmt"
    "log"
    "os"

    _ "github.com/jackc/pgx/v5/stdlib"
    "github.com/jmoiron/sqlx"
)

func NewDB() *sqlx.DB {

    dsn := fmt.Sprintf(
        "host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
        getEnv("DB_HOST", "localhost"),
        getEnv("DB_PORT", "5432"),
        getEnv("DB_USER", "postgres"),
        getEnv("DB_PASSWORD", "postgres"),
        getEnv("DB_NAME", "walletdb"),
        getEnv("DB_SSLMODE", "disable"),
    )

    db, err := sqlx.Connect("pgx", dsn)
    if err != nil {
        log.Fatal(err)
    }

    schema := `
    CREATE TABLE IF NOT EXISTS wallets (
        id TEXT PRIMARY KEY,
        balance BIGINT NOT NULL CHECK(balance >= 0)
    );

    CREATE TABLE IF NOT EXISTS transfers (
        id UUID PRIMARY KEY,
        idempotency_key TEXT UNIQUE NOT NULL,
        from_wallet_id TEXT NOT NULL,
        to_wallet_id TEXT NOT NULL,
        amount BIGINT NOT NULL CHECK(amount > 0),
        status TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT NOW()
    );

    CREATE TABLE IF NOT EXISTS ledger_entries (
        id BIGSERIAL PRIMARY KEY,
        wallet_id TEXT NOT NULL,
        transfer_id UUID NOT NULL,
        type TEXT NOT NULL,
        amount BIGINT NOT NULL CHECK(amount > 0)
    );
    `

    db.MustExec(schema)

    return db
}

func getEnv(key string, fallback string) string {
    value := os.Getenv(key)

    if value == "" {
        return fallback
    }

    return value
}
