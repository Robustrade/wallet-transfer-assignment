-- Wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id          VARCHAR(50)     PRIMARY KEY,
    owner_name  VARCHAR(255),
    owner_email VARCHAR(255),
    balance     NUMERIC(20, 2)  NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_by  VARCHAR(100)    NOT NULL DEFAULT 'system',
    updated_by  VARCHAR(100)    NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Transfers table
-- idempotency_key has a UNIQUE constraint — this is the primary idempotency guard
CREATE TABLE IF NOT EXISTS transfers (
    id              VARCHAR(50)     PRIMARY KEY,
    idempotency_key VARCHAR(255)    UNIQUE,
    from_wallet_id  VARCHAR(50)     NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(50)     NOT NULL REFERENCES wallets(id),
    amount          NUMERIC(20, 2)  NOT NULL CHECK (amount > 0),
    status          VARCHAR(20)     NOT NULL DEFAULT 'PENDING'
                                    CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_by      VARCHAR(100)    NOT NULL DEFAULT 'system',
    updated_by      VARCHAR(100)    NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet   ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status      ON transfers(status);

-- Ledger entries table — double-entry bookkeeping
CREATE TABLE IF NOT EXISTS ledger_entries (
    id          VARCHAR(50)     PRIMARY KEY,
    wallet_id   VARCHAR(50)     NOT NULL REFERENCES wallets(id),
    transfer_id VARCHAR(50)     NOT NULL REFERENCES transfers(id),
    type        VARCHAR(10)     NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      NUMERIC(20, 2)  NOT NULL CHECK (amount > 0),
    created_by  VARCHAR(100)    NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);

-- Seed wallets for testing
INSERT INTO wallets (id, owner_name, owner_email, balance) VALUES
    ('wallet_1', 'Alice', 'alice@example.com', 1000.00),
    ('wallet_2', 'Bob', 'bob@example.com', 500.00),
    ('wallet_3', 'Charlie', 'charlie@example.com', 250.00)
ON CONFLICT (id) DO NOTHING;
