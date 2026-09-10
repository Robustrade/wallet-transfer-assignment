-- Create wallets table
CREATE TABLE IF NOT EXISTS wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency    VARCHAR(3)  NOT NULL DEFAULT 'USD',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for querying wallets by currency (optional, but useful)
CREATE INDEX IF NOT EXISTS idx_wallets_currency ON wallets(currency);
