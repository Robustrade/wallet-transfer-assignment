-- 001_init.sql
-- Wallet Transfer Service schema

BEGIN;

-- ==========================================================================
-- Wallets
-- ==========================================================================
CREATE TABLE IF NOT EXISTS wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ==========================================================================
-- Transfers
-- ==========================================================================
CREATE TABLE IF NOT EXISTS transfers (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT NOT NULL,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL DEFAULT 'PENDING'
                         CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Idempotency: one transfer per key
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),

    -- Cannot transfer to self
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to   ON transfers(to_wallet_id);

-- ==========================================================================
-- Ledger Entries (double-entry bookkeeping)
-- ==========================================================================
CREATE TABLE IF NOT EXISTS ledger_entries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id   UUID NOT NULL REFERENCES transfers(id),
    wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    entry_type    TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount        BIGINT NOT NULL CHECK (amount > 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet   ON ledger_entries(wallet_id);

-- ==========================================================================
-- Seed data for demonstration / testing
-- ==========================================================================
INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 10000),
    ('wallet_2', 10000),
    ('wallet_3', 5000)
ON CONFLICT (id) DO NOTHING;

COMMIT;
