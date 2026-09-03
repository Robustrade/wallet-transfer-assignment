CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT   PRIMARY KEY,
    balance    BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id             TEXT   PRIMARY KEY,
    from_wallet_id TEXT   NOT NULL REFERENCES wallets (id),
    to_wallet_id   TEXT   NOT NULL REFERENCES wallets (id),
    amount         BIGINT NOT NULL CHECK (amount > 0),
    status         TEXT   NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at     TIMESTAMP NOT NULL,
    updated_at     TIMESTAMP NOT NULL,
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_reason_matches_status CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL) OR
        (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id, created_at);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id, created_at);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          TEXT   PRIMARY KEY,
    transfer_id TEXT   NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT   NOT NULL REFERENCES wallets (id),
    entry_type  TEXT   NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMP NOT NULL,
    CONSTRAINT ledger_entries_one_per_side UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries (wallet_id, created_at);

CREATE TABLE IF NOT EXISTS idempotency_records (
    key          TEXT PRIMARY KEY,
    request_hash TEXT NOT NULL,
    transfer_id  TEXT NOT NULL REFERENCES transfers (id),
    created_at   TIMESTAMP NOT NULL
);
