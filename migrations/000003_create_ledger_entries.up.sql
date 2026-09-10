-- Create ledger_entries table (double-entry bookkeeping)
CREATE TABLE IF NOT EXISTS ledger_entries (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID        NOT NULL REFERENCES transfers(id) ON DELETE CASCADE,
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets(id),
    entry_type  VARCHAR(8)  NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet   ON ledger_entries(wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_created  ON ledger_entries(created_at);
