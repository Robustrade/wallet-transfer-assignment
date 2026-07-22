-- Wallet Transfer Service schema.
-- wallet.balance holds the current balance; ledger_entries is the permanent record of moves.
-- Transfers run in the background: PENDING -> PROCESSING -> PROCESSED | FAILED
-- (the reaper puts a stuck PROCESSING transfer back to PENDING).
-- Correctness rules live in the database (constraints, unique keys, row locks), not in code.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Lookup tables: foreign keys to these mean only valid states and types can be stored.
CREATE TABLE IF NOT EXISTS transfer_state (
    state VARCHAR(12) PRIMARY KEY
);
INSERT INTO transfer_state (state) VALUES
    ('PENDING'), ('PROCESSING'), ('PROCESSED'), ('FAILED')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS transaction_type (
    type VARCHAR(10) PRIMARY KEY
);
INSERT INTO transaction_type (type) VALUES
    ('DEBIT'), ('CREDIT')
ON CONFLICT DO NOTHING;

-- updated_at maintained by the DB so no writer can forget it.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- balance_non_negative is the final safety net against overdrafts. Money is NUMERIC, never float.
CREATE TABLE IF NOT EXISTS wallet (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    balance    NUMERIC(20, 4) NOT NULL DEFAULT 0,
    is_active  BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT balance_non_negative CHECK (balance >= 0)
);

CREATE OR REPLACE TRIGGER wallet_set_updated_at
    BEFORE UPDATE ON wallet
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- transfer holds the request and also acts as the duplicate-check record. It keeps its own
-- from/to/amount, so a repeated request can return the original result and a FAILED transfer
-- (which writes no ledger rows) can still be read back. The unique idempotency_key stops
-- duplicates. claimed_at/worker_id/attempts are used by the background worker and the reaper.
CREATE TABLE IF NOT EXISTS transfer (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    idempotency_key VARCHAR(64) UNIQUE NOT NULL,
    from_wallet_id  UUID NOT NULL REFERENCES wallet(id),
    to_wallet_id    UUID NOT NULL REFERENCES wallet(id),
    amount          NUMERIC(20, 4) NOT NULL,
    state           VARCHAR(12) NOT NULL DEFAULT 'PENDING' REFERENCES transfer_state(state),
    failure_reason  TEXT,
    attempts        INT NOT NULL DEFAULT 0,
    claimed_at      TIMESTAMPTZ,
    worker_id       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfer_amount_positive CHECK (amount > 0),
    CONSTRAINT transfer_distinct_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE OR REPLACE TRIGGER transfer_set_updated_at
    BEFORE UPDATE ON transfer
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Partial indexes: they only cover unprocessed rows, so the worker and reaper queries stay small.
CREATE INDEX IF NOT EXISTS idx_transfer_pending
    ON transfer (created_at) WHERE state = 'PENDING';
CREATE INDEX IF NOT EXISTS idx_transfer_processing
    ON transfer (claimed_at) WHERE state = 'PROCESSING';

-- ledger_entries is the permanent record: one DEBIT and one CREDIT per transfer, never changed.
-- UNIQUE(transfer_id, type) makes reprocessing safe: a repeated entry fails instead of doubling.
CREATE TABLE IF NOT EXISTS ledger_entries (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    wallet_id   UUID NOT NULL REFERENCES wallet(id),
    transfer_id UUID NOT NULL REFERENCES transfer(id),
    type        VARCHAR(10) NOT NULL REFERENCES transaction_type(type),
    amount      NUMERIC(20, 4) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_one_entry_per_type UNIQUE (transfer_id, type)
);

-- Postgres does not index foreign key columns automatically; this speeds up lookups by wallet.
CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries (wallet_id);
