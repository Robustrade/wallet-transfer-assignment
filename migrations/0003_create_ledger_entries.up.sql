CREATE TABLE ledger_entries (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id BIGINT NOT NULL REFERENCES transfers (id),
    wallet_id   VARCHAR(36) NOT NULL REFERENCES wallets (id),
    entry_type  VARCHAR(6) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      DECIMAL(18, 2) NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
