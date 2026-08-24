-- Transfers. id is a UUIDv7 generated application-side (ADR-0004) — no DEFAULT here.
-- The self-transfer and failure-reason checks are defense-in-depth behind the same rules
-- the Transfer domain entity enforces at construction (ADR-0002, ADR-0006).
CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    from_wallet_id UUID NOT NULL REFERENCES wallets (id),
    to_wallet_id UUID NOT NULL REFERENCES wallets (id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK (status <> 'FAILED' OR failure_reason IS NOT NULL)
);

-- Support the optional transfer-history read API without a full scan (ADR-0004).
CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers (to_wallet_id);
