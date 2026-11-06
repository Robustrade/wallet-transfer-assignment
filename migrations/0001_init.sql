-- 0001_init: wallets, transfers, double-entry ledger and idempotency records.
-- Invariants are enforced here, not only in application code (see docs/DESIGN.md §3).

CREATE TABLE wallets (
    id         TEXT        PRIMARY KEY CHECK (char_length(id) BETWEEN 1 AND 64),
    balance    BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Backstop against double spend: even a buggy code path cannot overdraw a wallet.
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id             UUID        PRIMARY KEY,
    from_wallet_id TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    amount         BIGINT      NOT NULL,
    status         TEXT        NOT NULL,
    failure_reason TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    -- A FAILED transfer always carries a reason; PENDING/PROCESSED never do.
    CONSTRAINT transfers_failure_reason_matches_status
        CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

-- Wallet transfer history and FK lookups.
CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at);
CREATE INDEX transfers_to_wallet_created_idx ON transfers (to_wallet_id, created_at);

CREATE TABLE ledger_entries (
    id          BIGINT      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    type        TEXT        NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_type_valid CHECK (type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    -- At most one DEBIT and one CREDIT per transfer (also indexes transfer_id lookups).
    CONSTRAINT ledger_entries_one_per_side UNIQUE (transfer_id, type)
);

-- Wallet statement / balance reconciliation queries.
CREATE INDEX ledger_entries_wallet_created_idx ON ledger_entries (wallet_id, created_at);

CREATE TABLE idempotency_records (
    key          TEXT        PRIMARY KEY CHECK (char_length(key) BETWEEN 1 AND 255),
    request_hash TEXT        NOT NULL,
    -- The key is claimed before the transfer row exists (it serialises duplicates first),
    -- so the FK is checked at COMMIT. NOT NULL + UNIQUE: exactly one transfer per key.
    transfer_id  UUID        NOT NULL UNIQUE
                             REFERENCES transfers (id) DEFERRABLE INITIALLY DEFERRED,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
