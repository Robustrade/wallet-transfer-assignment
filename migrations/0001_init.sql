-- Wallet Transfer Service schema.

CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets (id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    state           TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    entry_type  TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries (transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries (wallet_id);

-- One row per idempotency key. The primary key is what makes claiming a
-- key an atomic, race-free operation: INSERT ... ON CONFLICT DO NOTHING
-- lets exactly one concurrent request win for a given key.
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    transfer_id     TEXT REFERENCES transfers (id),
    response_status INT,
    response_body   JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
