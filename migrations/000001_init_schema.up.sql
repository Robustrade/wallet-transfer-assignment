-- Create extensions if needed
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- 1. WALLETS TABLE
CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY,
    balance NUMERIC(20, 4) NOT NULL DEFAULT 0.0000,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_wallets_balance_non_negative CHECK (balance >= 0.0000)
);

-- 2. TRANSFERS TABLE
CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NULL,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount NUMERIC(20, 4) NOT NULL,
    status VARCHAR(20) NOT NULL,
    error_code VARCHAR(50) NULL,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ NULL,
    CONSTRAINT chk_transfers_amount_positive CHECK (amount > 0.0000),
    CONSTRAINT chk_transfers_different_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfers_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

-- 3. LEDGER_ENTRIES TABLE
CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type VARCHAR(10) NOT NULL,
    amount NUMERIC(20, 4) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_ledger_amount_positive CHECK (amount > 0.0000),
    CONSTRAINT chk_ledger_type CHECK (type IN ('DEBIT', 'CREDIT'))
);

-- 4. IDEMPOTENCY_RECORDS TABLE
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    transfer_id UUID NULL REFERENCES transfers(id),
    response_code INT NOT NULL,
    response_body JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- INDEXES FOR FAST SEARCH AND LOOKUP
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers(idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE UNIQUE INDEX IF NOT EXISTS ux_ledger_entries_transfer_type ON ledger_entries(transfer_id, type);
