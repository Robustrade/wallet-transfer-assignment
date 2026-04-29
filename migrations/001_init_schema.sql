-- Create wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    balance BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT positive_balance CHECK (balance >= 0)
);

-- Create transfers table
CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL,
    state VARCHAR(20) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT positive_amount CHECK (amount > 0),
    CONSTRAINT different_wallets CHECK (from_wallet_id != to_wallet_id),
    CONSTRAINT valid_state CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED'))
);

-- Create ledger_entries table
CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    entry_type VARCHAR(10) NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT valid_entry_type CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT positive_entry_amount CHECK (amount > 0),
    CONSTRAINT unique_transfer_entry_type UNIQUE (transfer_id, entry_type)
);

-- Create idempotency_records table
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    response_data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_transfers_state ON transfers(state);

-- Insert test wallets for development (using UUIDs)
INSERT INTO wallets (id, balance) VALUES 
    ('550e8400-e29b-41d4-a716-446655440001'::uuid, 1000),
    ('550e8400-e29b-41d4-a716-446655440002'::uuid, 500),
    ('550e8400-e29b-41d4-a716-446655440003'::uuid, 2000)
ON CONFLICT (id) DO NOTHING;
