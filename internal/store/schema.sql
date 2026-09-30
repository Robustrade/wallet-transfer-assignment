CREATE TABLE IF NOT EXISTS wallets (
    id TEXT PRIMARY KEY,
    balance BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id UUID PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, type)
);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    from_wallet_id TEXT NOT NULL,
    to_wallet_id TEXT NOT NULL,
    amount BIGINT NOT NULL,
    transfer_id UUID UNIQUE REFERENCES transfers(id),
    http_status SMALLINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (amount > 0),
    CHECK ((transfer_id IS NULL) = (http_status IS NULL))
);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_created_idx
    ON ledger_entries (wallet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS transfers_created_idx ON transfers (created_at DESC);

CREATE OR REPLACE FUNCTION enforce_transfer_status_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' THEN
            RAISE EXCEPTION 'transfers must be created in PENDING state';
        END IF;
    ELSIF NEW.from_wallet_id IS DISTINCT FROM OLD.from_wallet_id
       OR NEW.to_wallet_id IS DISTINCT FROM OLD.to_wallet_id
       OR NEW.amount IS DISTINCT FROM OLD.amount THEN
        RAISE EXCEPTION 'transfer business fields are immutable';
    ELSIF NEW.status <> OLD.status AND (OLD.status <> 'PENDING' OR NEW.status NOT IN ('PROCESSED', 'FAILED')) THEN
        RAISE EXCEPTION 'invalid transfer status transition: % -> %', OLD.status, NEW.status;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS transfers_status_transition ON transfers;
CREATE TRIGGER transfers_status_transition
    BEFORE INSERT OR UPDATE OF status, from_wallet_id, to_wallet_id, amount ON transfers
    FOR EACH ROW EXECUTE FUNCTION enforce_transfer_status_transition();

CREATE OR REPLACE FUNCTION assert_transfer_ledger_balanced() RETURNS trigger AS $$
DECLARE
    checked_transfer_id UUID;
    transfer_status TEXT;
    transfer_amount BIGINT;
    transfer_from_wallet_id TEXT;
    transfer_to_wallet_id TEXT;
    entry_count BIGINT;
    debit_total BIGINT;
    credit_total BIGINT;
    valid_debit_count BIGINT;
    valid_credit_count BIGINT;
BEGIN
    IF TG_TABLE_NAME = 'transfers' THEN
        checked_transfer_id := NEW.id;
    ELSIF TG_OP = 'DELETE' THEN
        checked_transfer_id := OLD.transfer_id;
    ELSE
        checked_transfer_id := NEW.transfer_id;
    END IF;

        SELECT status, amount, from_wallet_id, to_wallet_id
            INTO transfer_status, transfer_amount, transfer_from_wallet_id, transfer_to_wallet_id
            FROM transfers WHERE id = checked_transfer_id;
    IF NOT FOUND THEN
        RETURN NULL;
    END IF;
    IF transfer_status = 'PENDING' THEN
        RAISE EXCEPTION 'transfer % cannot commit in PENDING state', checked_transfer_id;
    END IF;
    IF transfer_status <> 'PROCESSED' THEN
        RETURN NULL;
    END IF;

    SELECT COUNT(*),
           COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0),
                     COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0),
                     COUNT(*) FILTER (
                             WHERE type = 'DEBIT'
                                 AND wallet_id = transfer_from_wallet_id
                                 AND amount = transfer_amount
                     ),
                     COUNT(*) FILTER (
                             WHERE type = 'CREDIT'
                                 AND wallet_id = transfer_to_wallet_id
                                 AND amount = transfer_amount
                     )
            INTO entry_count, debit_total, credit_total, valid_debit_count, valid_credit_count
      FROM ledger_entries WHERE transfer_id = checked_transfer_id;
        IF entry_count <> 2
             OR debit_total <> transfer_amount
             OR credit_total <> transfer_amount
             OR valid_debit_count <> 1
             OR valid_credit_count <> 1 THEN
                RAISE EXCEPTION 'transfer % must have one debit and credit matching its amount and wallets', checked_transfer_id;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS transfers_must_finish ON transfers;
CREATE CONSTRAINT TRIGGER transfers_must_finish
    AFTER INSERT OR UPDATE ON transfers
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_transfer_ledger_balanced();

DROP TRIGGER IF EXISTS ledger_entries_must_balance ON ledger_entries;
CREATE CONSTRAINT TRIGGER ledger_entries_must_balance
    AFTER INSERT OR UPDATE OR DELETE ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_transfer_ledger_balanced();

CREATE OR REPLACE FUNCTION reject_ledger_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ledger_entries_are_immutable ON ledger_entries;
CREATE TRIGGER ledger_entries_are_immutable
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
