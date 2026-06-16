CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TYPE transfer_status AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key   TEXT NOT NULL UNIQUE,
    from_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id      TEXT NOT NULL REFERENCES wallets(id),
    amount            BIGINT NOT NULL CHECK (amount > 0),
    status            transfer_status NOT NULL DEFAULT 'PENDING',
    failure_reason    TEXT,
    response_snapshot JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL PRIMARY KEY,
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    entry_type   ledger_entry_type NOT NULL,
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_transfers_from_wallet_created ON transfers(from_wallet_id, created_at);

INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 10000),
    ('wallet_2', 5000);
