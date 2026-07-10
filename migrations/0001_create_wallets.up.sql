CREATE TABLE wallets (
    id         VARCHAR(36) PRIMARY KEY,
    balance    DECIMAL(18, 2) NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
