CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    currency VARCHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_code_matches_status CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL AND BTRIM(failure_code) <> '')
        OR (status IN ('PENDING', 'PROCESSED') AND failure_code IS NULL)
    )
);

CREATE INDEX transfers_from_wallet_created_idx
    ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx
    ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ledger_entries_one_type_per_transfer UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_created_idx
    ON ledger_entries (wallet_id, created_at DESC);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL CHECK (length(request_hash) = 64 AND request_hash ~ '^[0-9a-f]{64}$'),
    transfer_id TEXT REFERENCES transfers(id) ON DELETE RESTRICT,
    response_status INTEGER,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT idempotency_response_complete CHECK (
        (response_status IS NULL) = (response_body IS NULL)
        AND (response_status IS NULL OR response_status BETWEEN 100 AND 599)
    )
);
