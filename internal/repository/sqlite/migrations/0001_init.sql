CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    INTEGER NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    amount          INTEGER NOT NULL CHECK (amount > 0),
    status          TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    request_hash    TEXT NOT NULL,
    created_at      TIMESTAMP NOT NULL,
    updated_at      TIMESTAMP NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    type        TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      INTEGER NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status ON transfers(status);
