-- +goose Up
CREATE TABLE wallets (
    id         TEXT        PRIMARY KEY,
    balance    BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    idempotency_key TEXT,
    from_wallet_id  TEXT        NOT NULL,
    to_wallet_id    TEXT        NOT NULL,
    amount          BIGINT      NOT NULL,
    status          TEXT        NOT NULL,
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
    CONSTRAINT transfers_idempotency_key_length CHECK (char_length(idempotency_key) BETWEEN 1 AND 255),
    CONSTRAINT transfers_from_wallet_fk FOREIGN KEY (from_wallet_id) REFERENCES wallets (id),
    CONSTRAINT transfers_to_wallet_fk   FOREIGN KEY (to_wallet_id)   REFERENCES wallets (id),
    CONSTRAINT transfers_amount_positive  CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid     CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason_iff_failed CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

-- Transfer history per wallet (either side), newest first.
CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx   ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID        NOT NULL,
    wallet_id   TEXT        NOT NULL,
    entry_type  TEXT        NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_transfer_fk FOREIGN KEY (transfer_id) REFERENCES transfers (id),
    CONSTRAINT ledger_entries_wallet_fk   FOREIGN KEY (wallet_id)   REFERENCES wallets (id),
    CONSTRAINT ledger_entries_type_valid      CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    -- At most one DEBIT and one CREDIT per transfer; the unique index also serves lookups by transfer_id.
    CONSTRAINT ledger_entries_one_side_per_transfer UNIQUE (transfer_id, entry_type)
);

-- Wallet statement: entries for one wallet in insertion order.
CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);

-- +goose Down
DROP TABLE ledger_entries;
DROP TABLE transfers;
DROP TABLE wallets;
