-- V1: Create wallets table
CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     NUMERIC(18, 4) NOT NULL DEFAULT 0
                    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

