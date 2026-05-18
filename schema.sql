CREATE TABLE wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance DECIMAL(15, 2) NOT NULL DEFAULT 0.00 CHECK (balance >= 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfers (
    id VARCHAR(255) PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    from_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    amount DECIMAL(15, 2) NOT NULL CHECK (amount > 0),
    status VARCHAR(50) NOT NULL, -- 'PENDING', 'PROCESSED', 'FAILED'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ledger_entries (
    id SERIAL PRIMARY KEY,
    transfer_id VARCHAR(255) NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    type VARCHAR(50) NOT NULL, -- 'DEBIT', 'CREDIT'
    amount DECIMAL(15, 2) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE idempotency_keys (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    locked_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    response_code INT,
    response_body JSONB
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_ledger_entries_transfer ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_entries_wallet ON ledger_entries(wallet_id);
