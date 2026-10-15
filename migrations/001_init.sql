-- 001_init.sql
-- Schema for the wallet transfer service.
-- See solution.md for design rationale.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT NOT NULL,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

-- Enforces exactly-once semantics at the API level: a duplicate
-- idempotencyKey can never insert a second transfer row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_transfers_idempotency_key
    ON transfers (idempotency_key);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    entry_type   TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id
    ON ledger_entries (transfer_id);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id
    ON ledger_entries (wallet_id);
