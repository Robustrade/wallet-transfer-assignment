CREATE TYPE transfer_status AS ENUM (
    'PENDING',
    'PROCESSED',
    'FAILED'
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),

    amount BIGINT NOT NULL CHECK (amount > 0),

    status transfer_status NOT NULL DEFAULT 'PENDING',

    failure_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_different_wallets
        CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_from_wallet
ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet
ON transfers(to_wallet_id);