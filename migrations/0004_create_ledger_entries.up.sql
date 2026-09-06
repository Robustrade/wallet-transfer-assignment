CREATE TABLE IF NOT EXISTS ledger_entries (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id  UUID NOT NULL REFERENCES transfers(id),
    wallet_id    UUID NOT NULL REFERENCES wallets(id),
    entry_type   TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount       NUMERIC(20,2) NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A transfer can have at most one DEBIT and one CREDIT entry.
    -- This is what actually prevents duplicate ledger entries at the DB level.
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries(transfer_id);
