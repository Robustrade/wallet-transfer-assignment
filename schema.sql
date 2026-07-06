-- 1. Wallets Table
CREATE TABLE wallets (
    id VARCHAR(255) PRIMARY KEY,
    balance NUMERIC(15, 4) NOT NULL DEFAULT 0.0000,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_positive_balance CHECK (balance >= 0)
);

-- 2. Transfers Table
-- It only cares about business data: amounts, wallet IDs, and state machine statuses (PENDING, PROCESSED, FAILED).
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    to_wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    amount NUMERIC(15, 4) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_positive_amount CHECK (amount > 0),
    CONSTRAINT chk_no_self_transfer CHECK (from_wallet_id <> to_wallet_id)
);

-- 3. Ledger Entries Table
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id VARCHAR(255) NOT NULL REFERENCES wallets(id),
    type VARCHAR(10) NOT NULL, -- 'DEBIT' or 'CREDIT'
    amount NUMERIC(15, 4) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_ledger_amount CHECK (amount > 0),
    CONSTRAINT chk_ledger_type CHECK (type IN ('DEBIT', 'CREDIT'))
);

-- 4. Idempotency Records Table
-- It only cares about network data: response_code (e.g., 200, 422, 500) and response_body (the raw JSON payload).
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL, -- SHA256 of payload to ensure data payload integrity
    response_code INT NOT NULL,
    response_body TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);


INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000.00);
INSERT INTO wallets (id, balance) VALUES ('wallet_2', 500.00);