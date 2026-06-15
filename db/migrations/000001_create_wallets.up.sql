CREATE TABLE wallets (
    id         TEXT        NOT NULL,
    balance    BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version    BIGINT      NOT NULL DEFAULT 0,

    CONSTRAINT wallets_pkey PRIMARY KEY (id),
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

-- Seed wallets for development and testing.
INSERT INTO wallets (id, balance) VALUES
    ('wallet_alice', 100000),
    ('wallet_bob',   50000),
    ('wallet_carol',     0);
