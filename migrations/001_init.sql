-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    wallet_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    balance_cents BIGINT NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT balance_non_negative CHECK (balance_cents >= 0),
    CONSTRAINT currency_not_empty CHECK (currency != ''),
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at)
);

-- Transfers table (state machine)
CREATE TABLE IF NOT EXISTS transfers (
    transfer_id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id) ON DELETE CASCADE,
    to_wallet_id UUID NOT NULL REFERENCES wallets(wallet_id) ON DELETE CASCADE,
    amount_cents BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING'
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_reason VARCHAR(500),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT amount_positive CHECK (amount_cents > 0),
    CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id),
    INDEX idx_from_wallet (from_wallet_id),
    INDEX idx_to_wallet (to_wallet_id),
    INDEX idx_status (status),
    INDEX idx_created_at (created_at)
);

-- Ledger entries table (double-entry bookkeeping)
CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id) ON DELETE CASCADE,
    wallet_id UUID NOT NULL REFERENCES wallets(wallet_id) ON DELETE CASCADE,
    entry_type VARCHAR(10) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount_cents BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    INDEX idx_transfer (transfer_id),
    INDEX idx_wallet (wallet_id),
    INDEX idx_created_at (created_at)
);

-- Idempotency records table (exactly-once semantics)
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(transfer_id) ON DELETE CASCADE,
    response_body JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE (idempotency_key)
);

-- Create indexes for frequently queried combinations
CREATE INDEX IF NOT EXISTS idx_transfers_from_status ON transfers(from_wallet_id, status);
CREATE INDEX IF NOT EXISTS idx_transfers_to_status ON transfers(to_wallet_id, status);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_wallet ON ledger_entries(transfer_id, wallet_id);
CREATE INDEX IF NOT EXISTS idx_wallets_user_id_created ON wallets(user_id, created_at);
