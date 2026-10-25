-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    currency TEXT NOT NULL,
    balance INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    CONSTRAINT chk_wallet_balance CHECK (balance >= 0)
);

CREATE INDEX IF NOT EXISTS idx_wallets_user_id ON wallets(user_id);

-- Transfers table (represents high-level transfer intent)
CREATE TABLE IF NOT EXISTS transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'COMPLETED', 'FAILED')),
    created_at DATETIME NOT NULL,
    CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);

-- Ledger entries table (immutable double-entry audit trail)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount INTEGER NOT NULL CHECK (amount > 0),
    direction TEXT NOT NULL CHECK (direction IN ('DEBIT', 'CREDIT')),
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);

-- Idempotency store (lease-based concurrency and response caching)
CREATE TABLE IF NOT EXISTS idempotency_records (
    key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'RESOLVED', 'FAILED')),
    response_code INTEGER,
    response_body TEXT,
    created_at DATETIME NOT NULL,
    locked_until DATETIME NOT NULL
);
