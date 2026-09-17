CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    customer_id UUID          NOT NULL,
    name        TEXT,
    status      TEXT          NOT NULL CHECK (status IN ('ACTIVE', 'INACTIVE', 'BLOCKED')),
    balance     NUMERIC(15, 4) NOT NULL DEFAULT 0 CHECK (balance >= 0),
    meta        TEXT,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
);

-- Get all wallets for a customer.
CREATE INDEX idx_wallets_customer_id ON wallets (customer_id);

CREATE TABLE transfers (
    id              UUID PRIMARY KEY,
    idempotency_key TEXT           NOT NULL UNIQUE,
    from_wallet_id  TEXT           NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT           NOT NULL REFERENCES wallets (id),
    initiated_by    UUID           NOT NULL,
    amount          NUMERIC(15, 4) NOT NULL CHECK (amount > 0),
    status          TEXT           NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_reason_iff_failed CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

-- Get all transfers initiated by a customer. No index on status: low cardinality.
CREATE INDEX idx_transfers_initiated_by ON transfers (initiated_by);

CREATE TABLE ledger_entries (
    id          UUID PRIMARY KEY,
    transfer_id UUID           NOT NULL REFERENCES transfers (id),
    customer_id UUID           NOT NULL,
    wallet_id   TEXT           NOT NULL REFERENCES wallets (id),
    type        TEXT           NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      NUMERIC(15, 4) NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_one_per_type UNIQUE (transfer_id, type)
);

CREATE INDEX idx_ledger_entries_customer_id ON ledger_entries (customer_id);
CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
