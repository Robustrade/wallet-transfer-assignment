CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    INTEGER NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transfers (
    id               TEXT PRIMARY KEY,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           INTEGER NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    idempotency_key  TEXT NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers(idempotency_key);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          TEXT PRIMARY KEY,
    wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    entry_type  TEXT NOT NULL CHECK (entry_type IN ('DEBIT','CREDIT')),
    amount      INTEGER NOT NULL CHECK (amount > 0),
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id     TEXT NOT NULL REFERENCES transfers(id),
    response_status INTEGER NOT NULL,
    response_body   TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
