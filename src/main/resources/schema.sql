CREATE TABLE IF NOT EXISTS wallets (
    wallet_id VARCHAR(100) PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transfers (
    transfer_id VARCHAR(100) PRIMARY KEY,
    idempotency_key VARCHAR(200) NOT NULL UNIQUE,
    from_wallet_id VARCHAR(100) NOT NULL,
    to_wallet_id VARCHAR(100) NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    state VARCHAR(20) NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (from_wallet_id) REFERENCES wallets (wallet_id),
    FOREIGN KEY (to_wallet_id) REFERENCES wallets (wallet_id),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id BIGSERIAL PRIMARY KEY,
    wallet_id VARCHAR(100) NOT NULL,
    transfer_id VARCHAR(100) NOT NULL,
    entry_type VARCHAR(20) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (wallet_id) REFERENCES wallets (wallet_id),
    FOREIGN KEY (transfer_id) REFERENCES transfers (transfer_id),
    UNIQUE (transfer_id, wallet_id, entry_type)
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(200) PRIMARY KEY,
    request_hash VARCHAR(64) NOT NULL,
    transfer_id VARCHAR(100),
    http_status INTEGER,
    response_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (transfer_id) REFERENCES transfers (transfer_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries (transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries (wallet_id);
