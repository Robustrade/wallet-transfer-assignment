CREATE TABLE wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance DECIMAL(15, 2) NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfers (
    id VARCHAR(255) PRIMARY KEY,
    from_wallet_id VARCHAR(255) REFERENCES wallets(id),
    to_wallet_id VARCHAR(255) REFERENCES wallets(id),
    amount DECIMAL(15, 2) NOT NULL,
    state VARCHAR(50) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE ledger_entries (
    id VARCHAR(255) PRIMARY KEY,
    wallet_id VARCHAR(255) REFERENCES wallets(id),
    transfer_id VARCHAR(255) REFERENCES transfers(id),
    entry_type VARCHAR(10) NOT NULL,
    amount DECIMAL(15, 2) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_payload JSONB NOT NULL,
    response_status INT,
    response_body JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Insert some dummy wallets for testing
INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000.00);
INSERT INTO wallets (id, balance) VALUES ('wallet_2', 500.00);
