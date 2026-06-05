-- V3: Create ledger_entries table
CREATE TABLE ledger_entries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    type        TEXT NOT NULL CONSTRAINT ledger_entries_type_valid CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      NUMERIC(18, 4) NOT NULL CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Guarantees exactly one DEBIT and one CREDIT per transfer (DB-level enforcement)
    CONSTRAINT uq_ledger_transfer_type UNIQUE (transfer_id, type)
);

CREATE INDEX idx_ledger_wallet    ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_transfer  ON ledger_entries (transfer_id);

