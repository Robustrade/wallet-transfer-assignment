CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers(to_wallet_id);
