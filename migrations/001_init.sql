CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    balance  NUMERIC(20, 2) NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now()
);

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL,
    to_wallet_id TEXT NOT NULL,
    amount  NUMERIC(20, 2) NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT now(),
    updated_at TIMESTAMP DEFAULT now(),

    -- Foreign Keys
    CONSTRAINT fk_from_wallet FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),
    CONSTRAINT fk_to_wallet FOREIGN KEY (to_wallet_id) REFERENCES wallets(id)
);

CREATE TABLE ledger_entries (
    id SERIAL PRIMARY KEY,
    wallet_id TEXT NOT NULL,
    transfer_id TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount NUMERIC(20, 2) NOT NULL,
    created_at TIMESTAMP DEFAULT now(),

    --  Foreign Keys
    CONSTRAINT fk_wallet FOREIGN KEY (wallet_id) REFERENCES wallets(id),
    CONSTRAINT fk_transfer FOREIGN KEY (transfer_id) REFERENCES transfers(id)
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    request_body JSONB NOT NULL,
    response JSONB,
    created_at TIMESTAMP DEFAULT now()
);

CREATE INDEX idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);