-- Wallet Transfer Service schema (SQLite).
-- Constraints, not application code, are the last line of defense.

CREATE TABLE IF NOT EXISTS wallets (
    id               TEXT    PRIMARY KEY,
    balance          INTEGER NOT NULL CHECK (balance >= 0),
    opening_balance  INTEGER NOT NULL CHECK (opening_balance >= 0),
    version          INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id               TEXT    PRIMARY KEY,
    idempotency_key  TEXT    NOT NULL,
    from_wallet_id   TEXT    NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT    NOT NULL REFERENCES wallets(id),
    amount           INTEGER NOT NULL CHECK (amount > 0),
    status           TEXT    NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT    NOT NULL DEFAULT '',
    created_at       TEXT    NOT NULL,
    updated_at       TEXT    NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS transfers_idempotency_key_uidx
    ON transfers (idempotency_key);

CREATE INDEX IF NOT EXISTS transfers_from_wallet_idx
    ON transfers (from_wallet_id);

CREATE INDEX IF NOT EXISTS transfers_to_wallet_idx
    ON transfers (to_wallet_id);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id           TEXT    PRIMARY KEY,
    transfer_id  TEXT    NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT    NOT NULL REFERENCES wallets(id),
    entry_type   TEXT    NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       INTEGER NOT NULL CHECK (amount > 0),
    created_at   TEXT    NOT NULL
);

-- A transfer can have at most one debit and one credit.
CREATE UNIQUE INDEX IF NOT EXISTS ledger_entries_transfer_type_uidx
    ON ledger_entries (transfer_id, entry_type);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_idx
    ON ledger_entries (wallet_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key  TEXT PRIMARY KEY,
    transfer_id      TEXT NOT NULL REFERENCES transfers(id),
    request_hash     TEXT NOT NULL,
    created_at       TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idempotency_records_transfer_uidx
    ON idempotency_records (transfer_id);
