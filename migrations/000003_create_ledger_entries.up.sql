CREATE TABLE ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    transfer_id BIGINT NOT NULL,
    wallet_id BIGINT NOT NULL,

    entry_type VARCHAR(10) NOT NULL,
    amount BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_ledger_transfer
        FOREIGN KEY (transfer_id) REFERENCES transfers(id),

    CONSTRAINT fk_ledger_wallet
        FOREIGN KEY (wallet_id) REFERENCES wallets(id),

    CONSTRAINT chk_ledger_entry_type
        CHECK (entry_type IN ('DEBIT', 'CREDIT')),

    CONSTRAINT chk_ledger_amount
        CHECK (amount > 0),

    CONSTRAINT uq_ledger_transfer_entry_type
        UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_wallet_id
    ON ledger_entries(wallet_id);