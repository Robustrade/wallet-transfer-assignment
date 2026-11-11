BEGIN;
-- Serialize explicit migration invocations; nothing runs on HTTP startup.
SELECT pg_advisory_xact_lock(734921001);

CREATE TABLE schema_migrations (
    singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    version INTEGER NOT NULL CHECK (version > 0),
    dirty BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE wallets (
    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 128),
    opening_balance BIGINT NOT NULL CHECK (opening_balance >= 0),
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id),
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id),
    CHECK ((status = 'FAILED' AND failure_reason IS NOT NULL) OR
           (status <> 'FAILED' AND failure_reason IS NULL))
);

CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id),
    wallet_id TEXT NOT NULL REFERENCES wallets(id),
    type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, type)
);
CREATE INDEX ledger_entries_wallet_id_id ON ledger_entries(wallet_id, id);

CREATE TABLE idempotency_records (
    idempotency_key VARCHAR(128) PRIMARY KEY CHECK (length(idempotency_key) > 0),
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    transfer_id UUID NOT NULL UNIQUE REFERENCES transfers(id),
    response_status INTEGER CHECK (response_status BETWEEN 200 AND 499),
    -- Text preserves the exact response bytes; JSONB would normalize them.
    response_body TEXT CHECK (response_body IS NULL OR jsonb_typeof(response_body::jsonb) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((response_status IS NULL) = (response_body IS NULL))
);

CREATE FUNCTION protect_transfer() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'transfers cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' THEN
            RAISE EXCEPTION 'transfers must begin pending' USING ERRCODE = '23514';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status <> 'PENDING' OR
       ROW(NEW.id, NEW.from_wallet_id, NEW.to_wallet_id, NEW.amount, NEW.created_at)
       IS DISTINCT FROM ROW(OLD.id, OLD.from_wallet_id, OLD.to_wallet_id, OLD.amount, OLD.created_at) THEN
        RAISE EXCEPTION 'terminal state and transfer identity are immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_transfer BEFORE INSERT OR UPDATE OR DELETE ON transfers
FOR EACH ROW EXECUTE FUNCTION protect_transfer();

CREATE FUNCTION protect_ledger() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_status TEXT;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'ledger is append-only' USING ERRCODE = '23514';
    END IF;
    -- Serialize ledger insertion with terminal transitions, including direct SQL.
    SELECT status INTO current_status FROM transfers WHERE id = NEW.transfer_id FOR UPDATE;
    IF current_status <> 'PENDING' THEN
        RAISE EXCEPTION 'ledger can only be added to pending transfers' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_ledger BEFORE INSERT OR UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION protect_ledger();

CREATE FUNCTION protect_idempotency() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'idempotency records cannot be deleted' USING ERRCODE = '23514';
    END IF;
    IF ROW(NEW.idempotency_key, NEW.request_hash, NEW.transfer_id, NEW.created_at)
       IS DISTINCT FROM ROW(OLD.idempotency_key, OLD.request_hash, OLD.transfer_id, OLD.created_at)
       OR OLD.response_status IS NOT NULL THEN
        RAISE EXCEPTION 'idempotency identity and terminal response are immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER protect_idempotency BEFORE UPDATE OR DELETE ON idempotency_records
FOR EACH ROW EXECUTE FUNCTION protect_idempotency();

CREATE FUNCTION validate_transfer_commit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id UUID;
    t transfers%ROWTYPE;
    entry_count BIGINT;
    matching_count BIGINT;
    saved_status INTEGER;
BEGIN
    IF TG_TABLE_NAME = 'transfers' THEN target_id := NEW.id;
    ELSE target_id := NEW.transfer_id;
    END IF;
    SELECT * INTO t FROM transfers WHERE id = target_id FOR UPDATE;
    SELECT count(*), count(*) FILTER (WHERE amount = t.amount AND
        ((type = 'DEBIT' AND wallet_id = t.from_wallet_id) OR
         (type = 'CREDIT' AND wallet_id = t.to_wallet_id)))
    INTO entry_count, matching_count FROM ledger_entries WHERE transfer_id = target_id;
    IF (t.status = 'PROCESSED' AND (entry_count <> 2 OR matching_count <> 2)) OR
       (t.status <> 'PROCESSED' AND entry_count <> 0) THEN
        RAISE EXCEPTION 'transfer ledger pair invalid' USING ERRCODE = '23514';
    END IF;
    SELECT response_status INTO saved_status FROM idempotency_records WHERE transfer_id = target_id;
    IF NOT FOUND OR
       (t.status = 'PENDING' AND saved_status IS NOT NULL) OR
       (t.status = 'PROCESSED' AND saved_status IS DISTINCT FROM 201) OR
       (t.status = 'FAILED' AND saved_status IS DISTINCT FROM 422) THEN
        RAISE EXCEPTION 'transfer response does not match state' USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;
CREATE CONSTRAINT TRIGGER validate_transfer_commit AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_transfer_commit();
CREATE CONSTRAINT TRIGGER validate_ledger_commit AFTER INSERT ON ledger_entries
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_transfer_commit();
CREATE CONSTRAINT TRIGGER validate_idempotency_commit AFTER INSERT OR UPDATE ON idempotency_records
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_transfer_commit();

INSERT INTO schema_migrations (version, dirty) VALUES (1, false);
COMMIT;
