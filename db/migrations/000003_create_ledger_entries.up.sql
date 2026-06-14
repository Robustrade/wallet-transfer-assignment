CREATE TYPE entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE ledger_entries (
    id          UUID        NOT NULL DEFAULT gen_random_uuid(),
    transfer_id UUID        NOT NULL,
    wallet_id   TEXT        NOT NULL,
    type        entry_type  NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT ledger_entries_pkey        PRIMARY KEY (id),
    CONSTRAINT ledger_entries_transfer_fk FOREIGN KEY (transfer_id) REFERENCES transfers (id),
    CONSTRAINT ledger_entries_wallet_fk   FOREIGN KEY (wallet_id)   REFERENCES wallets (id),
    CONSTRAINT ledger_entries_amount_pos  CHECK (amount > 0)
);

CREATE INDEX idx_ledger_entries_transfer ON ledger_entries (transfer_id);
CREATE INDEX idx_ledger_entries_wallet   ON ledger_entries (wallet_id);
