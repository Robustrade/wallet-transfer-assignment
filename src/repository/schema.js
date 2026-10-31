// PostgreSQL schema. Idempotent (safe to run on every start).
export const SCHEMA = `
CREATE TABLE IF NOT EXISTS wallets (
    id              text        PRIMARY KEY,
    initial_balance bigint      NOT NULL CHECK (initial_balance >= 0),
    balance         bigint      NOT NULL CHECK (balance >= 0),   -- never negative, even if app code is buggy
    created_at      timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS transfers (
    id              uuid        PRIMARY KEY,
    idempotency_key text        UNIQUE,                          -- NULLs allowed (many NULLs OK in UNIQUE)
    from_wallet_id  text        NOT NULL REFERENCES wallets(id),
    to_wallet_id    text        NOT NULL REFERENCES wallets(id),
    amount          bigint      NOT NULL CHECK (amount > 0),
    status          text        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  text,
    created_at      timestamptz NOT NULL,
    updated_at      timestamptz NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_transfers_from ON transfers (from_wallet_id, created_at);
CREATE INDEX IF NOT EXISTS idx_transfers_to   ON transfers (to_wallet_id, created_at);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id   text        NOT NULL REFERENCES wallets(id),
    transfer_id uuid        NOT NULL REFERENCES transfers(id),
    type        text        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      bigint      NOT NULL CHECK (amount > 0),
    created_at  timestamptz NOT NULL,
    UNIQUE (transfer_id, type)                                   -- at most one DEBIT and one CREDIT per transfer
);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON ledger_entries (wallet_id, id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    key          text        PRIMARY KEY,                        -- the dedupe guarantee
    request_hash text        NOT NULL,
    transfer_id  uuid        NOT NULL UNIQUE REFERENCES transfers(id),
    created_at   timestamptz NOT NULL
);

-- Defence in depth: terminal states can never change.
CREATE OR REPLACE FUNCTION transfer_status_guard() RETURNS trigger AS $$
BEGIN
    IF OLD.status <> 'PENDING' THEN
        RAISE EXCEPTION 'illegal transfer state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_transfer_status_guard ON transfers;
CREATE TRIGGER trg_transfer_status_guard
    BEFORE UPDATE OF status ON transfers
    FOR EACH ROW EXECUTE FUNCTION transfer_status_guard();

-- The ledger is append-only.
CREATE OR REPLACE FUNCTION ledger_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_ledger_immutable ON ledger_entries;
CREATE TRIGGER trg_ledger_immutable
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_immutable();
`;
