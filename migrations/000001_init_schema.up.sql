CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              UUID PRIMARY KEY,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          TEXT NOT NULL CHECK (status IN ('PENDING','PROCESSED','FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);
-- CREATE INDEX idx_transfers_from_wallet_failed
--     ON transfers (from_wallet_id, created_at)
--     WHERE status = 'FAILED';
-- this can be implemented if the UI requires failed transfer history

CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY,
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    type         TEXT NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, type)
);
CREATE INDEX idx_ledger_entries_wallet_time ON ledger_entries (wallet_id, created_at DESC);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    transfer_id     UUID NOT NULL UNIQUE REFERENCES transfers(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
