CREATE TABLE wallets (
    id VARCHAR(100) PRIMARY KEY,
    balance NUMERIC(19, 4) NOT NULL CHECK (balance >= 0),
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(100) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(100) NOT NULL REFERENCES wallets(id),
    amount NUMERIC(19, 4) NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason VARCHAR(500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR(100) NOT NULL REFERENCES wallets(id),
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount NUMERIC(19, 4) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id, created_at DESC);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id, created_at DESC);
CREATE INDEX idx_ledger_wallet ON ledger_entries(wallet_id, created_at DESC);