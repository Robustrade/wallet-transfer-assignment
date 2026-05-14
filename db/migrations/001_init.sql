-- ============================================================
-- 001_init.sql — Wallet Transfer Service schema
-- ============================================================

-- Wallets hold a running balance.
-- balance is stored in the smallest currency unit (e.g. cents).
-- The CHECK constraint is an extra safeguard; the application also
-- validates sufficient funds before issuing an UPDATE.
CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT        PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Transfers represent a request to move funds between two wallets.
-- idempotency_key has a UNIQUE constraint so that duplicate requests
-- (retries, network replays) are detected at the database level and
-- cannot create duplicate transfer records.
CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT        PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL DEFAULT 'PENDING'
                                CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_transfers_idempotency_key
    ON transfers(idempotency_key);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet
    ON transfers(from_wallet_id);

CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet
    ON transfers(to_wallet_id);

-- Ledger entries implement double-entry bookkeeping.
-- Every transfer produces exactly two rows: one DEBIT (source) and one CREDIT (dest).
-- The foreign key to transfers ensures entries can never be orphaned.
CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGSERIAL   PRIMARY KEY,
    transfer_id TEXT        NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets(id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet
    ON ledger_entries(wallet_id);

CREATE INDEX IF NOT EXISTS idx_ledger_transfer
    ON ledger_entries(transfer_id);
