-- Wallet transfer service schema.
-- Design goals: append-only ledger as source of truth, a materialized wallet balance
-- for O(1) reads, and DB-enforced idempotency + integrity constraints.

CREATE TABLE wallets (
    -- UUID v7 assigned by the application (Hibernate UuidGenerator.Style.TIME) on insert.
    id          UUID           PRIMARY KEY,
    balance     NUMERIC(19, 4) NOT NULL DEFAULT 0,
    currency    TEXT           NOT NULL DEFAULT 'INR',
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    -- UUID v7 assigned by the application (Hibernate UuidGenerator.Style.TIME) on insert.
    id              UUID           PRIMARY KEY,
    from_wallet_id  UUID           NOT NULL REFERENCES wallets (id),
    to_wallet_id    UUID           NOT NULL REFERENCES wallets (id),
    amount          NUMERIC(19, 4) NOT NULL,
    status          TEXT           NOT NULL,
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE TABLE ledger_entries (
    -- UUID v7 assigned by the application (Hibernate UuidGenerator.Style.TIME) on insert.
    id            UUID           PRIMARY KEY,
    wallet_id     UUID           NOT NULL REFERENCES wallets (id),
    transfer_id   UUID           NOT NULL REFERENCES transfers (id),
    type          TEXT           NOT NULL,
    amount        NUMERIC(19, 4) NOT NULL,
    balance_after NUMERIC(19, 4) NOT NULL,
    created_at    TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_entries_type_valid CHECK (type IN ('DEBIT', 'CREDIT'))
);

-- Idempotency lookups and per-wallet / per-transfer history queries.
CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers (to_wallet_id);
CREATE INDEX idx_ledger_entries_wallet ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer ON ledger_entries (transfer_id);
