CREATE TABLE wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0, -- whole rupees (INR)
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    idempotency_key  TEXT NOT NULL UNIQUE,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets (id),
    amount           BIGINT NOT NULL CHECK (amount > 0), -- whole rupees (INR)
    status           TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id != to_wallet_id)
);

CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    entry_type  TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0), -- whole rupees (INR)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, entry_type)
);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id     TEXT NOT NULL REFERENCES transfers (id),
    response_body   JSONB,
    http_status     INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers (to_wallet_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);
