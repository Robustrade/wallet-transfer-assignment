-- wallets: stored balance, updated atomically in transfer transaction
CREATE TABLE wallets (
    id         VARCHAR(64) PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    version    BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- transfers: one row per logical transfer attempt (keyed by idempotency)
CREATE TABLE transfers (
    id              UUID PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL UNIQUE,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          VARCHAR(16) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);

-- ledger: exactly 2 rows per PROCESSED transfer
CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    entry_type  VARCHAR(8) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_entries_transfer ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_entries_wallet ON ledger_entries(wallet_id);

-- idempotency: stores serialized response for exact replay
CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(128) PRIMARY KEY,
    transfer_id     UUID NOT NULL REFERENCES transfers(id),
    request_hash    VARCHAR(64) NOT NULL,
    response_body   JSONB NOT NULL,
    http_status     INT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
