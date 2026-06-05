CREATE TABLE transfers (

    id UUID PRIMARY KEY,

    idempotency_key VARCHAR(255)
        NOT NULL
        UNIQUE,

    from_wallet_id UUID
        NOT NULL
        REFERENCES wallets(id),

    to_wallet_id UUID
        NOT NULL
        REFERENCES wallets(id),

    amount BIGINT
        NOT NULL
        CHECK(amount > 0),

    state VARCHAR(32)
        NOT NULL,

    created_at TIMESTAMP
        NOT NULL
        DEFAULT NOW(),

    updated_at TIMESTAMP
        NOT NULL
        DEFAULT NOW(),

    CHECK (
        from_wallet_id <> to_wallet_id
    )
);

CREATE INDEX idx_transfer_state
ON transfers(state);

CREATE INDEX idx_transfer_from_wallet
ON transfers(from_wallet_id);

CREATE INDEX idx_transfer_to_wallet
ON transfers(to_wallet_id);