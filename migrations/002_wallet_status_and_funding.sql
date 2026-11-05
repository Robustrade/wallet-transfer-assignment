BEGIN TRANSACTION;

-- Add wallet lifecycle fields.
ALTER TABLE wallets ADD COLUMN status TEXT NOT NULL DEFAULT 'ACTIVE'
    CHECK (status IN ('ACTIVE', 'INACTIVE'));
ALTER TABLE wallets ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
UPDATE wallets SET updated_at = CURRENT_TIMESTAMP WHERE updated_at = '';

-- Funding and opening-balance credits are ledger entries without a transfer.
-- Rebuild the table to make transfer_id nullable while preserving existing rows.
CREATE TABLE ledger_entries_new (
    id TEXT PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    transfer_id TEXT REFERENCES transfers(id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount INTEGER NOT NULL CHECK (amount > 0),
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (transfer_id, type)
);
INSERT INTO ledger_entries_new (id, wallet_id, transfer_id, type, amount, created_at)
SELECT id, wallet_id, transfer_id, type, amount, created_at FROM ledger_entries;
DROP TABLE ledger_entries;
ALTER TABLE ledger_entries_new RENAME TO ledger_entries;
CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);

COMMIT;
