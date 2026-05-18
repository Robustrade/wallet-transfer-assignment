BEGIN;

CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id TEXT PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    transfer_id TEXT REFERENCES transfers(id),
    response_code INT,
    response_body JSONB,
    status TEXT NOT NULL CHECK (status IN ('STARTED', 'COMPLETED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX ux_ledger_transfer_type
ON ledger_entries (transfer_id, type);

CREATE INDEX idx_ledger_entries_wallet_id
ON ledger_entries(wallet_id);

CREATE INDEX idx_ledger_entries_transfer_id
ON ledger_entries(transfer_id);

CREATE INDEX idx_transfers_from_wallet_id
ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet_id
ON transfers(to_wallet_id);

CREATE INDEX idx_transfers_status
ON transfers(status);

COMMIT;