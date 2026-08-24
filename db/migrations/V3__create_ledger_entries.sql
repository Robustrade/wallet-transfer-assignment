-- Ledger entries. id is a UUIDv7 generated application-side (ADR-0004) — no DEFAULT here.
-- transfer_id NOT NULL makes "a ledger row with no transfer" unrepresentable (ADR-0004).
CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers (id),
    wallet_id UUID NOT NULL REFERENCES wallets (id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexed for balance-reconciliation and history lookups (ADR-0004).
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);
