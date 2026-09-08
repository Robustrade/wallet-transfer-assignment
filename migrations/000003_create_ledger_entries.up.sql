CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id UUID NOT NULL REFERENCES wallets(id),
    entry_type ledger_entry_type NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (transfer_id, wallet_id, entry_type)
);
CREATE INDEX ledger_entries_transfer_idx ON ledger_entries(transfer_id);
CREATE INDEX ledger_entries_wallet_idx ON ledger_entries(wallet_id, created_at DESC);
