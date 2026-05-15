CREATE TYPE ledger_entry_type AS ENUM (
    'CREDIT',
    'DEBIT'
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id) ON DELETE CASCADE,
    amount BIGINT NOT NULL CHECK (amount > 0),
    entry_type ledger_entry_type NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);

CREATE UNIQUE INDEX idx_ledger_entries_transfer_entry_type ON ledger_entries(transfer_id, entry_type);