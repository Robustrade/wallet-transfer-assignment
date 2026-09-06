CREATE TABLE IF NOT EXISTS transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id  UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallets(id),
    amount          NUMERIC(20,2) NOT NULL CHECK (amount > 0),
    status          TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status ON transfers(status);
