CREATE TABLE IF NOT EXISTS wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS transfers (
    id VARCHAR(255) PRIMARY KEY,
    from_wallet_id VARCHAR(255) NOT NULL,
    to_wallet_id VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    state VARCHAR(50) NOT NULL,
    idempotency_key VARCHAR(255) UNIQUE,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),
    FOREIGN KEY (to_wallet_id) REFERENCES wallets(id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id VARCHAR(255) PRIMARY KEY,
    wallet_id VARCHAR(255) NOT NULL,
    transfer_id VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (wallet_id) REFERENCES wallets(id),
    FOREIGN KEY (transfer_id) REFERENCES transfers(id)
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP NOT NULL
);
