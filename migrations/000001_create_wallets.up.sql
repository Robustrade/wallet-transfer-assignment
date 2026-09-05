CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL CHECK (char_length(btrim(name)) > 0),
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD' CHECK (char_length(currency) = 3),
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
