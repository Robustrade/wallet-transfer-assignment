-- Wallet Transfer Service — Schema
-- Run with: psql -U postgres -d wallet_db -f schema.sql

BEGIN;

-- ─────────────────────────────────────────────
-- wallets
-- ─────────────────────────────────────────────
CREATE TABLE wallets (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_name      TEXT NOT NULL,
    balance         BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    -- optimistic-lock helper; also useful for auditing "last touched"
    version         INTEGER NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─────────────────────────────────────────────
-- transfers
-- ─────────────────────────────────────────────
CREATE TYPE transfer_state AS ENUM ('PENDING', 'PROCESSED', 'FAILED');

CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    state           transfer_state NOT NULL DEFAULT 'PENDING',
    failure_reason  TEXT,
    idempotency_key TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

-- one transfer per idempotency key — this is the core dedup guarantee
CREATE UNIQUE INDEX uq_transfers_idempotency_key ON transfers (idempotency_key);

-- fast lookups for wallet history / balance recompute
CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id);

-- ─────────────────────────────────────────────
-- ledger_entries  (double-entry bookkeeping)
-- ─────────────────────────────────────────────
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE ledger_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id     UUID NOT NULL REFERENCES transfers(id),
    wallet_id       UUID NOT NULL REFERENCES wallets(id),
    type            ledger_entry_type NOT NULL,
    amount          BIGINT NOT NULL CHECK (amount > 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- guarantees exactly one DEBIT row and one CREDIT row per (transfer, wallet)
    -- and prevents a retried/duplicated write path from inserting the same
    -- leg of the ledger twice
    CONSTRAINT uq_ledger_transfer_wallet_type UNIQUE (transfer_id, wallet_id, type)
);

CREATE INDEX idx_ledger_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_transfer_id ON ledger_entries (transfer_id);

-- ─────────────────────────────────────────────
-- idempotency_records
-- ─────────────────────────────────────────────
-- Stores the *result* of a request so repeated calls with the same key
-- return the original response instead of re-running business logic.
-- Separate from transfers.idempotency_key (which prevents duplicate rows)
-- because this table stores the full response payload / status, and can
-- later be reused for other idempotent endpoints beyond just /transfers.
CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash    TEXT NOT NULL,        -- hash of request body, to detect key reuse with different payload
    transfer_id     UUID REFERENCES transfers(id),
    response_body   JSONB,
    status_code     INTEGER,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;