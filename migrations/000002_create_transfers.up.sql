-- Create transfers table
CREATE TABLE IF NOT EXISTS transfers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key VARCHAR(255) NOT NULL,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          VARCHAR(16) NOT NULL DEFAULT 'PENDING'
                    CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Unique constraint on idempotency key for deduplication
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),

    -- Prevent self-transfers at the DB level
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet   ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status      ON transfers(status);
CREATE INDEX IF NOT EXISTS idx_transfers_created_at  ON transfers(created_at);
