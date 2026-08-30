CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets (id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX transfers_from_wallet_id_idx ON transfers (from_wallet_id);
CREATE INDEX transfers_to_wallet_id_idx ON transfers (to_wallet_id);

CREATE TABLE ledger_entries (
    id TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers (id),
    wallet_id TEXT NOT NULL REFERENCES wallets (id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_transfer_type_unique UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_transfer_id_idx ON ledger_entries (transfer_id);
CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    transfer_id TEXT REFERENCES transfers (id),
    http_status INTEGER,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
