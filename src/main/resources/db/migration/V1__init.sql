-- V1: Initial wallet transfer schema.
--
-- Design notes:
--  * Amounts use BIGINT minor units (e.g. paise/cents) to avoid float rounding.
--  * Single currency. Multi-currency is out of scope.
--  * CHECK (balance >= 0) is the structural guarantee against double-spend even
--    if application logic is buggy.
--  * UNIQUE (transfer_id, type) on ledger_entries guarantees at most one DEBIT
--    and at most one CREDIT per transfer. The service writes both entries in the
--    same DB transaction as the transfer's PROCESSED state transition, so every
--    PROCESSED transfer has exactly one of each.
--  * idempotency_records.key is the natural PK; the FK to transfers ties the
--    cached response to the side effects performed under that key.

CREATE TABLE wallets (
    id          VARCHAR(64)  PRIMARY KEY,
    balance     BIGINT       NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id              UUID         PRIMARY KEY,
    from_wallet_id  VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    amount          BIGINT       NOT NULL CHECK (amount > 0),
    status          VARCHAR(16)  NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    failure_reason  VARCHAR(64),
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);

CREATE TABLE ledger_entries (
    id           BIGSERIAL    PRIMARY KEY,
    wallet_id    VARCHAR(64)  NOT NULL REFERENCES wallets(id),
    transfer_id  UUID         NOT NULL REFERENCES transfers(id),
    type         VARCHAR(8)   NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
    amount       BIGINT       NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, type)
);
CREATE INDEX idx_ledger_wallet_created ON ledger_entries(wallet_id, created_at DESC);

CREATE TABLE idempotency_records (
    key              VARCHAR(64)  PRIMARY KEY,
    request_hash     VARCHAR(64)  NOT NULL CHECK (char_length(request_hash) = 64),
    transfer_id      UUID         NOT NULL REFERENCES transfers(id),
    response_status  SMALLINT,
    response_body    TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_idempotency_transfer ON idempotency_records(transfer_id);

-- ── Seed data ────────────────────────────────────────────────────────────────
-- Two wallets so that a fresh `docker compose up` is immediately usable
-- against the sample requests documented in the README. wallet_1 holds an
-- initial balance; wallet_2 starts empty.
INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 10000),
    ('wallet_2', 0);
