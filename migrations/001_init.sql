CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    id BIGSERIAL PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_transfer_id
    ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_entries_wallet_id
    ON ledger_entries(wallet_id);
    