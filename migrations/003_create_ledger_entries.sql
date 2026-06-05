CREATE TABLE ledger_entries (

    id UUID PRIMARY KEY,

    transfer_id UUID
        NOT NULL
        REFERENCES transfers(id),

    wallet_id UUID
        NOT NULL
        REFERENCES wallets(id),

    entry_type VARCHAR(16)
        NOT NULL,

    amount BIGINT
        NOT NULL
        CHECK(amount > 0),

    created_at TIMESTAMP
        NOT NULL
        DEFAULT NOW()
);

--Entry Type Constraint
ALTER TABLE ledger_entries
ADD CONSTRAINT chk_ledger_entry_type
CHECK (
    entry_type IN (
        'DEBIT',
        'CREDIT'
    )
);

--Ledger Lookup Indexes
CREATE INDEX idx_ledger_transfer
ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_wallet
ON ledger_entries(wallet_id);

--Debit Constraint
CREATE UNIQUE INDEX
uq_transfer_debit
ON ledger_entries(
    transfer_id,
    entry_type
)
WHERE entry_type='DEBIT';

--Credit Constraint
CREATE UNIQUE INDEX
uq_transfer_credit
ON ledger_entries(
    transfer_id,
    entry_type
)
WHERE entry_type='CREDIT';