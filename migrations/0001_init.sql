BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets (
    id          TEXT        PRIMARY KEY,
    balance     BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Balance must never go negative (no overdraft / double-spend at DB level).
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'PENDING',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_status_valid    CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL   PRIMARY KEY,
    wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    transfer_id  UUID        NOT NULL REFERENCES transfers(id),
    type         TEXT        NOT NULL,
    amount       BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ledger_type_valid     CHECK (type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),

    -- A given transfer can produce at most one entry of each type per wallet.
    -- This makes ledger writes idempotent/retry-safe and prevents duplicates.
    CONSTRAINT ledger_unique_entry UNIQUE (transfer_id, wallet_id, type)
);

CREATE INDEX idx_ledger_wallet_id   ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_transfer_id ON ledger_entries (transfer_id);

CREATE TABLE idempotency_records (
    idempotency_key TEXT        PRIMARY KEY,
    transfer_id     UUID        NOT NULL REFERENCES transfers(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;