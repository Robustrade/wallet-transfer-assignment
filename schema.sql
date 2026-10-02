CREATE TABLE wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id VARCHAR(255) PRIMARY KEY,
    idempotency_key VARCHAR(255) UNIQUE NOT NULL,
    from_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL,
    state VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE ledger_entries (
    id VARCHAR(255) PRIMARY KEY,
    wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    transfer_id VARCHAR(255) NOT NULL REFERENCES transfers(id),
    type VARCHAR(50) NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE idempotency_records (
    key VARCHAR(255) PRIMARY KEY,
    response TEXT,
    status_code INT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
