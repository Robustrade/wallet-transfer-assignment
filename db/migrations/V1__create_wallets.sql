-- Wallets. id is a UUIDv7 generated application-side (ADR-0004) — no DEFAULT here.
-- balance is stored, not derived from the ledger (ADR-0009), in integer minor units.
CREATE TABLE wallets (
    id UUID PRIMARY KEY,
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
