CREATE INDEX IF NOT EXISTS idx_ledger_entry_wallet_created_at
    ON ledger_entry (wallet_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_ledger_entry_created_at
    ON ledger_entry (created_at DESC);
