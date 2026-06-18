CREATE TABLE wallets (
    id VARCHAR(36) PRIMARY KEY,
    balance DECIMAL(19,4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    version BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);


CREATE TABLE transfers (
    id VARCHAR(36) PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(36) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(36) NOT NULL REFERENCES wallets(id),
    amount DECIMAL(19,4) NOT NULL CHECK (amount > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    response_body TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);



CREATE TABLE ledger_entries (
    id VARCHAR(36) PRIMARY KEY,
    wallet_id VARCHAR(36) NOT NULL REFERENCES wallets(id),
    transfer_id VARCHAR(36) NOT NULL REFERENCES transfers(id),
    type VARCHAR(10) NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
    amount DECIMAL(19,4) NOT NULL CHECK (amount > 0),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);

INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000.0000);
INSERT INTO wallets (id, balance) VALUES ('wallet_2', 500.0000);