CREATE TABLE IF NOT EXISTS wallets (
    id         VARCHAR(64) PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency   VARCHAR(8) NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              UUID PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets (id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets (id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          VARCHAR(16) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_idempotency_key ON transfers (idempotency_key);
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers (id),
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets (id),
    entry_type  VARCHAR(8) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);

-- Replay does not need a stored response snapshot: the transfer row
-- referenced by transfer_id is already the durable source of truth, so a
-- replay is just "re-fetch that transfer and return it".
CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key VARCHAR(255) PRIMARY KEY,
    request_hash    VARCHAR(64) NOT NULL,
    status          VARCHAR(16) NOT NULL CHECK (status IN ('PENDING', 'COMPLETED')),
    transfer_id     UUID REFERENCES transfers (id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A COMPLETED record must be linked to the transfer it completed with --
    -- replay depends on that link (it re-fetches transfer_id, see comment
    -- above). Without this, a future code path that ever updates status
    -- without also setting transfer_id would durably corrupt a key: it
    -- would look "done" but have nothing valid to replay.
    CONSTRAINT chk_idempotency_completed_has_transfer
        CHECK (status <> 'COMPLETED' OR transfer_id IS NOT NULL)
);
