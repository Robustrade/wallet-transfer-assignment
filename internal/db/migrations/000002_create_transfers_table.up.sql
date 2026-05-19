
CREATE TABLE transfers (
    id UUID PRIMARY KEY,

    idempotency_key TEXT UNIQUE NOT NULL,

    from_wallet_id UUID NOT NULL REFERENCES wallets(id),
    to_wallet_id UUID NOT NULL REFERENCES wallets(id),

    amount BIGINT NOT NULL CHECK (amount > 0),

    status TEXT NOT NULL CHECK (
        status IN ('PENDING', 'PROCESSED', 'FAILED')
    ),

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);