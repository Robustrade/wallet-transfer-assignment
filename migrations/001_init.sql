-- Wallet Transfer Service Database Schema
-- This migration creates the core tables for the wallet transfer system.

BEGIN;

-- Enable pgcrypto extension for gen_random_uuid() on older PostgreSQL versions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================================
-- wallets: stores wallet balances with currency denomination
-- ============================================================================
CREATE TABLE IF NOT EXISTS wallets (
    id          VARCHAR(255) PRIMARY KEY,
    balance     BIGINT       NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency    VARCHAR(3)   NOT NULL DEFAULT 'USD',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- idempotency_records: gate/barrier checked BEFORE the transfers table
-- Prevents duplicate processing and detects mismatched duplicate requests.
--
-- NOTE: transfer_id is a logical reference to transfers(id) but intentionally
-- has NO foreign key constraint. This avoids a circular FK dependency
-- (transfers → idempotency_records via idempotency_key, and
-- idempotency_records → transfers via transfer_id) which would complicate
-- data cleanup, migrations, and bulk operations.
-- ============================================================================
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key  VARCHAR(255) PRIMARY KEY,
    request_hash     VARCHAR(64)  NOT NULL,
    transfer_id      UUID,
    status           VARCHAR(20)  NOT NULL DEFAULT 'IN_PROGRESS'
                     CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- transfers: core transfer records with PENDING → PROCESSED | FAILED states
-- ============================================================================
CREATE TABLE IF NOT EXISTS transfers (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  VARCHAR(255) NOT NULL UNIQUE
                     REFERENCES idempotency_records(idempotency_key),
    from_wallet_id   VARCHAR(255) NOT NULL REFERENCES wallets(id),
    to_wallet_id     VARCHAR(255) NOT NULL REFERENCES wallets(id),
    amount           BIGINT       NOT NULL CHECK (amount > 0),
    status           VARCHAR(20)  NOT NULL DEFAULT 'PENDING'
                     CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_reason     TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT no_self_transfer CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet   ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status      ON transfers(status);

-- ============================================================================
-- ledger_entries: double-entry bookkeeping (every processed transfer → 1 DEBIT + 1 CREDIT)
-- ============================================================================
CREATE TABLE IF NOT EXISTS ledger_entries (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID         NOT NULL REFERENCES transfers(id),
    wallet_id    VARCHAR(255) NOT NULL REFERENCES wallets(id),
    entry_type   VARCHAR(10)  NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT       NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT unique_transfer_entry_type UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet   ON ledger_entries(wallet_id);

COMMIT;
