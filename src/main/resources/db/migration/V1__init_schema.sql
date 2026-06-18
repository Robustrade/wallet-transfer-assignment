CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE wallets (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    balance     NUMERIC(19,4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key      VARCHAR(255) NOT NULL UNIQUE,
    request_fingerprint  VARCHAR(64) NOT NULL,
    from_wallet_id       UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id         UUID NOT NULL REFERENCES wallets(id),
    amount               NUMERIC(19,4) NOT NULL CHECK (amount > 0),
    status               VARCHAR(10) NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    failure_reason       TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_transfer_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    UUID NOT NULL REFERENCES wallets(id),
    entry_type   VARCHAR(6) NOT NULL CHECK (entry_type IN ('DEBIT','CREDIT')),
    amount       NUMERIC(19,4) NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_ledger_transfer_entry_type UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers (to_wallet_id);
