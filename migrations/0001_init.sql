-- Wallet transfer schema.
--
-- Money is stored as BIGINT minor units (e.g. paise/cents). The database is the
-- last line of defence: application bugs must not be able to overdraw a wallet,
-- duplicate a transfer, or leave an unbalanced ledger.

CREATE TABLE wallets (
    id              TEXT        PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),
    balance         BIGINT      NOT NULL CHECK (balance >= 0),
    -- Balance the wallet was opened with. Lets the invariant
    --   balance = opening_balance + sum(credits) - sum(debits)
    -- be verified from the database alone.
    opening_balance BIGINT      NOT NULL CHECK (opening_balance >= 0),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    -- Second line of defence behind idempotency_records: even a bug that skips
    -- the key claim cannot create two transfers for one key.
    idempotency_key TEXT        NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 255),
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id),
    -- A failure reason exists if and only if the transfer failed.
    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at);
CREATE INDEX transfers_to_wallet_created_idx   ON transfers (to_wallet_id, created_at);

-- Append-only double-entry ledger.
CREATE TABLE ledger_entries (
    id          BIGSERIAL   PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL,
    -- At most one DEBIT and one CREDIT per transfer.
    UNIQUE (transfer_id, type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);

-- Ledger rows can never be changed or removed.
CREATE FUNCTION ledger_entries_forbid_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_forbid_mutation();

-- At commit time every transfer that has ledger rows must have exactly one
-- DEBIT and one CREDIT of equal amount: the ledger always balances.
CREATE FUNCTION ledger_entries_check_balanced() RETURNS trigger AS $$
DECLARE
    entry_count  INT;
    debit_total  BIGINT;
    credit_total BIGINT;
BEGIN
    SELECT count(*),
           COALESCE(sum(amount) FILTER (WHERE type = 'DEBIT'), 0),
           COALESCE(sum(amount) FILTER (WHERE type = 'CREDIT'), 0)
      INTO entry_count, debit_total, credit_total
      FROM ledger_entries
     WHERE transfer_id = NEW.transfer_id;

    IF entry_count <> 2 OR debit_total <> credit_total THEN
        RAISE EXCEPTION 'unbalanced ledger for transfer %: % entries, debit %, credit %',
            NEW.transfer_id, entry_count, debit_total, credit_total;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_check_balanced();

-- Durable idempotency records. The primary key is the claim: a concurrent
-- duplicate blocks on the unique index until the first transaction finishes.
CREATE TABLE idempotency_records (
    key          TEXT        PRIMARY KEY CHECK (length(key) BETWEEN 1 AND 255),
    request_hash TEXT        NOT NULL,
    transfer_id  UUID        REFERENCES transfers (id),
    created_at   TIMESTAMPTZ NOT NULL
);
