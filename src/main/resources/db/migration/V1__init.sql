-- Wallet transfer schema (PostgreSQL).
-- Money is stored as BIGINT minor units. Every business invariant that can be expressed as a
-- constraint is enforced here, so a bug in application code cannot corrupt the ledger.

CREATE TABLE wallets (
    id              TEXT        PRIMARY KEY,
    balance         BIGINT      NOT NULL CHECK (balance >= 0),
    opening_balance BIGINT      NOT NULL CHECK (opening_balance >= 0),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE TABLE transfers (
    id              TEXT        PRIMARY KEY,
    idempotency_key TEXT,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    state           TEXT        NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    -- a failure reason is present exactly when the transfer FAILED
    CONSTRAINT transfers_failure_reason CHECK ((state = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id, created_at);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id, created_at);

CREATE TABLE ledger_entries (
    id          TEXT        PRIMARY KEY,
    transfer_id TEXT        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL,
    -- at most one DEBIT and one CREDIT per transfer
    CONSTRAINT ledger_one_entry_per_side UNIQUE (transfer_id, type)
);

CREATE INDEX idx_ledger_wallet ON ledger_entries (wallet_id, created_at);

-- The ledger is append-only.
CREATE FUNCTION forbid_ledger_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_immutable
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION forbid_ledger_mutation();

CREATE TABLE idempotency_records (
    key          TEXT        PRIMARY KEY,
    request_hash TEXT        NOT NULL,
    -- Deferred so a request can claim its key first and insert the transfer later in the same
    -- transaction; the constraint is checked at commit.
    transfer_id  TEXT        NOT NULL UNIQUE REFERENCES transfers (id)
                             DEFERRABLE INITIALLY DEFERRED,
    created_at   TIMESTAMPTZ NOT NULL
);
