package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    balance INTEGER NOT NULL CHECK (balance >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount INTEGER NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount INTEGER NOT NULL CHECK (amount > 0),
    created_at TEXT NOT NULL,
    UNIQUE (transfer_id, entry_type)
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    transfer_id TEXT REFERENCES transfers(id),
    status TEXT NOT NULL CHECK (status IN ('PROCESSING', 'COMPLETED')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_transfers_from ON transfers(from_wallet_id);
`
