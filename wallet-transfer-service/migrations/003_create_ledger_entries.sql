CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, wallet_id, type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_transfer ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries(wallet_id);
