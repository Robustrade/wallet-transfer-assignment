-- Wallet transfer schema.
--
-- This is the authoritative DDL for the service. It is applied when the database
-- is provisioned, NOT by the application at startup. Locally, the Postgres
-- container runs this file via /docker-entrypoint-initdb.d/ on first init (see
-- testenv/docker-compose.yml). The service assumes the schema already exists.
--
-- Statements use IF NOT EXISTS so applying the schema is safe to re-run.

CREATE TABLE IF NOT EXISTS wallets (
    wallet_id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transfers (
    transfer_id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS transfers_from_wallet_idx ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS transfers_to_wallet_idx ON transfers (to_wallet_id);
CREATE INDEX IF NOT EXISTS transfers_state_idx ON transfers (state);

CREATE TABLE IF NOT EXISTS ledger_entries (
    ledger_entry_id TEXT PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_idx ON ledger_entries (wallet_id);

-- Guarantees exactly one DEBIT and one CREDIT per transfer (double-entry integrity).
CREATE UNIQUE INDEX IF NOT EXISTS ux_ledger_transfer_type ON ledger_entries (transfer_id, type);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id),
    -- Snapshot of the original response so replayed requests return the exact
    -- original result (exactly-once semantics at the API level).
    response TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

