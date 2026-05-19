-- +goose Up
-- Exactly two entries per transfer (one DEBIT, one CREDIT). Amount is always
-- positive; direction is encoded in `type`. UNIQUE(transfer_id, type) caps a
-- transfer at one DEBIT and one CREDIT row.
CREATE TABLE ledger_entries (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    transfer_id UUID          NOT NULL REFERENCES transfers(id),
    wallet_id   TEXT          NOT NULL REFERENCES wallets(id),
    type        TEXT          NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      NUMERIC(18,2) NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),

    CONSTRAINT unique_entry_per_transfer_type UNIQUE (transfer_id, type)
);

-- +goose Down
DROP TABLE ledger_entries;
