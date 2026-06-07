CREATE TABLE IF NOT EXISTS wallets (
                                       id          TEXT PRIMARY KEY,
                                       balance     INTEGER NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency    TEXT NOT NULL DEFAULT 'USD',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
    );

CREATE TABLE IF NOT EXISTS transfers (
                                         id               TEXT PRIMARY KEY,
                                         idempotency_key  TEXT NOT NULL UNIQUE,
                                         from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           INTEGER NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
    );

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);

CREATE TABLE IF NOT EXISTS ledger_entries (
                                              id           TEXT PRIMARY KEY,
                                              transfer_id  TEXT NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    entry_type   TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       INTEGER NOT NULL CHECK (amount > 0),
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
    );

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);

INSERT OR IGNORE INTO wallets (id, balance, currency) VALUES
    ('wallet_1', 10000, 'USD'),
    ('wallet_2', 5000, 'USD'),
    ('wallet_3', 0, 'USD');