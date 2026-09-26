CREATE TABLE wallets (
    wallet_id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    key TEXT PRIMARY KEY,
    request_hash BYTEA NOT NULL CHECK (octet_length(request_hash) = 32),
    state TEXT NOT NULL CHECK (state IN ('IN_PROGRESS', 'COMPLETED')),
    response_status SMALLINT,
    response_body JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    CHECK ((state = 'IN_PROGRESS' AND response_status IS NULL AND response_body IS NULL AND completed_at IS NULL)
        OR (state = 'COMPLETED' AND response_status IS NOT NULL AND response_body IS NOT NULL AND completed_at IS NOT NULL))
);

CREATE TABLE transfers (
    transfer_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE REFERENCES idempotency_records(key),
    from_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    entry_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX transfers_from_history_idx ON transfers(from_wallet_id, created_at DESC, transfer_id DESC);
CREATE INDEX transfers_to_history_idx ON transfers(to_wallet_id, created_at DESC, transfer_id DESC);
CREATE INDEX ledger_entries_wallet_transfer_idx ON ledger_entries(wallet_id, transfer_id);