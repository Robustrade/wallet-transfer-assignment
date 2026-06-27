-- 0001_init.sql
-- Initial schema for the wallet transfer service.
--
-- Design notes (see docs/DESIGN.md for the full rationale):
--   * Money is stored as BIGINT in the smallest currency unit; CHECK constraints
--     forbid invalid amounts and overdrafts at the database boundary.
--   * Idempotency is enforced by the PRIMARY KEY on idempotency_records.key and,
--     defensively, by a UNIQUE constraint on transfers.idempotency_key.
--   * Double-entry integrity is protected by a UNIQUE (transfer_id, type)
--     constraint so a transfer can have at most one DEBIT and one CREDIT row.
--   * Foreign keys guarantee ledger rows and transfers always reference real
--     wallets, so orphan ledger entries are impossible.

CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              UUID PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          UUID PRIMARY KEY,
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- At most one DEBIT and one CREDIT per transfer: prevents duplicate or
    -- unbalanced ledger writes even under retries.
    CONSTRAINT ledger_entries_one_per_type UNIQUE (transfer_id, type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries (wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries (transfer_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    key           TEXT PRIMARY KEY,
    request_hash  TEXT        NOT NULL,
    transfer_id   UUID        REFERENCES transfers (id),
    response_body JSONB,
    status        TEXT        NOT NULL CHECK (status IN ('PENDING', 'COMPLETED')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
