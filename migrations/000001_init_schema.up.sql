-- 000001_init_schema.up.sql
-- Creates the initial schema for the wallet transfer service.

BEGIN;

-- ── wallets ──────────────────────────────────────────────────────────────────
-- Stores wallet identity and running balance.
-- balance is stored in the smallest currency unit (minor units / cents) as a
-- BIGINT to match the domain Amount type and avoid any floating-point concerns.
-- version supports optimistic-locking guard in application code.
CREATE TABLE wallets (
    id         UUID        PRIMARY KEY,
    balance    BIGINT      NOT NULL    DEFAULT 0
                           CHECK (balance >= 0),
    version    BIGINT      NOT NULL    DEFAULT 0,
    created_at TIMESTAMP   NOT NULL    DEFAULT NOW(),
    updated_at TIMESTAMP   NOT NULL    DEFAULT NOW()
);

-- ── transfers ─────────────────────────────────────────────────────────────────
-- Records a transfer request and its lifecycle.
-- idempotency_key carries a UNIQUE constraint so that any concurrent
-- requests with the same key race to insert; only one wins.
-- status is stored as a SMALLINT matching TransferStatus domain constants:
--   1 = PENDING, 2 = PROCESSED, 3 = FAILED
-- amount is stored in minor units (same as balance) as BIGINT.
CREATE TABLE transfers (
    id               UUID        PRIMARY KEY,
    idempotency_key  TEXT        NOT NULL,
    from_wallet_id   UUID        NOT NULL REFERENCES wallets(id),
    to_wallet_id     UUID        NOT NULL REFERENCES wallets(id),
    amount           BIGINT      NOT NULL CHECK (amount > 0),
    status           SMALLINT    NOT NULL DEFAULT 1
                                 CHECK (status IN (1, 2, 3)),
    created_at       TIMESTAMP   NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMP   NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

-- ── ledger_entries ────────────────────────────────────────────────────────────
-- Double-entry bookkeeping: every transfer produces exactly two rows.
-- type is stored as a SMALLINT matching EntryType domain constants:
--   1 = DEBIT (money leaving a wallet)
--   2 = CREDIT (money entering a wallet)
-- amount is stored in minor units as BIGINT.
CREATE TABLE ledger_entries (
    id          UUID        PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers(id),
    wallet_id   UUID        NOT NULL REFERENCES wallets(id),
    type        SMALLINT    NOT NULL CHECK (type IN (1, 2)),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMP   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_transfer ON ledger_entries (transfer_id);
CREATE INDEX idx_ledger_wallet   ON ledger_entries (wallet_id);

-- ── idempotency_records ───────────────────────────────────────────────────────
-- Caches the full HTTP response body and status code for each idempotency key.
-- A successful or failed transfer is written here within the same transaction
-- so that duplicate requests receive an identical response without re-executing
-- any business logic.
CREATE TABLE idempotency_records (
    key           TEXT        PRIMARY KEY,
    transfer_id   UUID        NOT NULL,
    response_json TEXT        NOT NULL,
    status_code   SMALLINT    NOT NULL,
    created_at    TIMESTAMP   NOT NULL DEFAULT NOW()
);

COMMIT;
