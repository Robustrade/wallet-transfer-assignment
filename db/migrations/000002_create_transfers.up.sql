CREATE TYPE transfer_status_type as ENUM (
    'PENDING',
    'PROCESSED',
    'FAILED'
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    to_wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    transfer_status transfer_status_type NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet_id ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers(to_wallet_id);
CREATE INDEX idx_transfers_status ON transfers(transfer_status);