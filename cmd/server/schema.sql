-- Schema DDL for PostgreSQL

CREATE TABLE IF NOT EXISTS wallets (
    id VARCHAR(64) PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transfers (
    id VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    from_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status VARCHAR(32) NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR(64) NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(16) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uniq_ledger_transfer_type UNIQUE (transfer_id, entry_type)
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL,
    response_code INT,
    response_body TEXT,
    transfer_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id ON ledger_entries(wallet_id);
