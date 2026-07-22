-- +goose Up

CREATE TYPE transfer_status AS ENUM (
    'PENDING',
    'PROCESSED',
    'FAILED'
);

CREATE TYPE ledger_type AS ENUM (
    'DEBIT',
    'CREDIT'
);

CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status transfer_status NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    entry_type ledger_type NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX ux_transfer_entry
ON ledger_entries (transfer_id, wallet_id, entry_type);

CREATE INDEX idx_transfer_idempotency
ON transfers(idempotency_key);

CREATE INDEX idx_wallet
ON ledger_entries(wallet_id);

-- +goose Down

DROP TABLE ledger_entries;
DROP TABLE transfers;
DROP TABLE wallets;

DROP TYPE ledger_type;
DROP TYPE transfer_status;