CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,

    wallet_id UUID NOT NULL REFERENCES wallets(id),

    transfer_id UUID NOT NULL REFERENCES transfers(id),

    entry_type TEXT NOT NULL CHECK (
        entry_type IN ('DEBIT', 'CREDIT')
    ),

    amount BIGINT NOT NULL CHECK (amount > 0),

    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);