-- =============================================================================
-- Wallet Transfer Service — Initial Schema Setup
-- Executed automatically by PostgreSQL on first container start.
-- All queries in this file constitute the full initial setup.
-- =============================================================================

-- ---------------------------------------------------------------------------
-- 1. Custom type
-- ---------------------------------------------------------------------------

CREATE TYPE entry_type AS ENUM ('DEBIT', 'CREDIT');

-- ---------------------------------------------------------------------------
-- 2. Tables
-- ---------------------------------------------------------------------------

CREATE TABLE wallets (
    id          BIGSERIAL    PRIMARY KEY,
    balance     BIGINT       NOT NULL DEFAULT 0,
    created_at  TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfers (
    id                TEXT    PRIMARY KEY,
    idempotency_key   TEXT    UNIQUE NOT NULL,
    from_wallet_id    BIGINT  NOT NULL REFERENCES wallets(id),
    to_wallet_id      BIGINT  NOT NULL REFERENCES wallets(id),
    amount            BIGINT  NOT NULL,
    status            TEXT    NOT NULL DEFAULT 'PENDING',
    failure_reason    TEXT,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL    PRIMARY KEY,
    transfer_id  TEXT         NOT NULL REFERENCES transfers(id),
    wallet_id    BIGINT       NOT NULL REFERENCES wallets(id),
    type         entry_type   NOT NULL,
    amount       BIGINT       NOT NULL,
    created_at   TIMESTAMP    DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE idempotency_records (
    idempotency_key   TEXT    PRIMARY KEY,
    response_status   INT     NOT NULL,
    response_body     TEXT    NOT NULL,
    created_at        TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- ---------------------------------------------------------------------------
-- 3. Indexes
-- (idempotency_key on transfers and idempotency_records already indexed
--  via UNIQUE / PRIMARY KEY constraints above)
-- ---------------------------------------------------------------------------

-- transfers: filter/join by wallet
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);

-- ledger_entries: filter by wallet (transaction history) and by transfer
CREATE INDEX idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);

-- ---------------------------------------------------------------------------
-- 4. Seed wallets (used by tests and local dev)
-- ---------------------------------------------------------------------------

INSERT INTO wallets (balance) VALUES
    (10000),   -- wallet id 1 — 100.00 units
    (10000),   -- wallet id 2
    (10000),   -- wallet id 3
    (0);       -- wallet id 4 — zero balance (useful for testing insufficient funds)
