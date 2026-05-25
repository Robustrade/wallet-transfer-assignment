CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id TEXT PRIMARY KEY DEFAULT ('le_' || replace(gen_random_uuid()::TEXT, '-', '')),
    wallet_id TEXT NOT NULL REFERENCES wallets(wallet_id),
    transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(transfer_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);