CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    idempotency_key  TEXT NOT NULL UNIQUE,
    from_wallet_id   TEXT NOT NULL,
    to_wallet_id     TEXT NOT NULL,
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_code     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT different_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT valid_failure_state CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL)
        OR (status <> 'FAILED' AND failure_code IS NULL)
    )
);

CREATE TABLE idempotency_records (
    idempotency_key  TEXT PRIMARY KEY,
    request_hash     CHAR(64) NOT NULL,
    transfer_id      TEXT UNIQUE REFERENCES transfers(id) ON DELETE RESTRICT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

CREATE TABLE ledger_entries (
    id           TEXT PRIMARY KEY,
    wallet_id    TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    transfer_id  TEXT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    entry_type   TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT one_entry_of_each_type_per_transfer UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_transfers_from_wallet_created_at
    ON transfers (from_wallet_id, created_at DESC);

CREATE INDEX idx_transfers_to_wallet_created_at
    ON transfers (to_wallet_id, created_at DESC);

CREATE INDEX idx_ledger_entries_wallet_created_at
    ON ledger_entries (wallet_id, created_at DESC);
