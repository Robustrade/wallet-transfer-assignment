-- +goose Up

CREATE TABLE wallets (
    id text PRIMARY KEY,
    initial_balance bigint NOT NULL,
    balance bigint NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT wallets_id_format_ck CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    CONSTRAINT wallets_initial_balance_ck CHECK (initial_balance >= 0),
    CONSTRAINT wallets_balance_ck CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id uuid PRIMARY KEY,
    idempotency_key text NOT NULL,
    from_wallet_id text NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    to_wallet_id text NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    amount bigint NOT NULL,
    status text NOT NULL,
    failure_reason text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT transfers_idempotency_key_format_ck CHECK (idempotency_key ~ '^[A-Za-z0-9_-]{1,128}$'),
    CONSTRAINT transfers_idempotency_key_key UNIQUE (idempotency_key),
    CONSTRAINT transfers_amount_ck CHECK (amount > 0),
    CONSTRAINT transfers_wallets_differ_ck CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_ck CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason_ck CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL AND length(failure_reason) > 0)
        OR (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id uuid PRIMARY KEY,
    wallet_id text NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    transfer_id uuid NOT NULL REFERENCES transfers (id) ON DELETE RESTRICT,
    entry_type text NOT NULL,
    amount bigint NOT NULL,
    created_at timestamptz NOT NULL,
    CONSTRAINT ledger_entries_type_ck CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_ck CHECK (amount > 0),
    CONSTRAINT ledger_entries_transfer_type_key UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_created_idx ON ledger_entries (wallet_id, created_at DESC);

CREATE TABLE idempotency_records (
    idempotency_key text PRIMARY KEY,
    request_hash text NOT NULL,
    transfer_id uuid UNIQUE REFERENCES transfers (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT idempotency_records_key_format_ck CHECK (idempotency_key ~ '^[A-Za-z0-9_-]{1,128}$'),
    CONSTRAINT idempotency_records_hash_ck CHECK (length(request_hash) > 0)
);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION check_transfer_ledger(p_transfer_id uuid)
RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    v_status text;
    v_amount bigint;
    v_from text;
    v_to text;
    v_debit_count integer;
    v_credit_count integer;
    v_debit_amount bigint;
    v_credit_amount bigint;
    v_debit_wallet text;
    v_credit_wallet text;
BEGIN
    SELECT status, amount, from_wallet_id, to_wallet_id
      INTO v_status, v_amount, v_from, v_to
      FROM public.transfers
     WHERE id = p_transfer_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'ledger invariant failed: transfer % is missing', p_transfer_id
            USING ERRCODE = '23514';
    END IF;

    SELECT
        COUNT(*) FILTER (WHERE entry_type = 'DEBIT'),
        COUNT(*) FILTER (WHERE entry_type = 'CREDIT'),
        COALESCE(SUM(amount) FILTER (WHERE entry_type = 'DEBIT'), 0),
        COALESCE(SUM(amount) FILTER (WHERE entry_type = 'CREDIT'), 0),
        MIN(wallet_id) FILTER (WHERE entry_type = 'DEBIT'),
        MIN(wallet_id) FILTER (WHERE entry_type = 'CREDIT')
      INTO v_debit_count, v_credit_count, v_debit_amount, v_credit_amount, v_debit_wallet, v_credit_wallet
      FROM public.ledger_entries
     WHERE transfer_id = p_transfer_id;

    IF v_status = 'PENDING' THEN
        RAISE EXCEPTION 'transfer % cannot commit while PENDING', p_transfer_id
            USING ERRCODE = '23514';
    ELSIF v_status = 'PROCESSED' THEN
        IF v_debit_count <> 1
           OR v_credit_count <> 1
           OR v_debit_amount <> v_amount
           OR v_credit_amount <> v_amount
           OR v_debit_wallet IS DISTINCT FROM v_from
           OR v_credit_wallet IS DISTINCT FROM v_to THEN
            RAISE EXCEPTION 'processed transfer % has an unbalanced ledger', p_transfer_id
                USING ERRCODE = '23514';
        END IF;
    ELSIF v_status = 'FAILED' THEN
        IF v_debit_count <> 0 OR v_credit_count <> 0 THEN
            RAISE EXCEPTION 'failed transfer % must not have ledger entries', p_transfer_id
                USING ERRCODE = '23514';
        END IF;
    ELSE
        RAISE EXCEPTION 'transfer % has unknown status %', p_transfer_id, v_status
            USING ERRCODE = '23514';
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION transfers_ledger_trigger()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    PERFORM public.check_transfer_ledger(NEW.id);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ledger_entries_trigger()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        PERFORM public.check_transfer_ledger(OLD.transfer_id);
    ELSE
        PERFORM public.check_transfer_ledger(NEW.transfer_id);
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION idempotency_requires_transfer()
RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog, public
AS $$
DECLARE
    v_transfer_id uuid;
BEGIN
    SELECT transfer_id
      INTO v_transfer_id
      FROM public.idempotency_records
     WHERE idempotency_key = NEW.idempotency_key;

    IF v_transfer_id IS NULL THEN
        RAISE EXCEPTION 'idempotency key % committed without a transfer', NEW.idempotency_key
            USING ERRCODE = '23514';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER transfers_ledger_balanced
AFTER INSERT OR UPDATE ON transfers
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION transfers_ledger_trigger();

CREATE CONSTRAINT TRIGGER ledger_entries_match_transfer
AFTER INSERT OR UPDATE OR DELETE ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION ledger_entries_trigger();

CREATE CONSTRAINT TRIGGER idempotency_requires_transfer_trg
AFTER INSERT OR UPDATE ON idempotency_records
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION idempotency_requires_transfer();

COMMENT ON TABLE wallets IS 'Stored balances. initial_balance is opening funding and is not a ledger row.';
COMMENT ON TABLE transfers IS 'One wallet-to-wallet attempt. Committed rows are PROCESSED or FAILED.';
COMMENT ON TABLE ledger_entries IS 'Double-entry postings. A processed transfer has one debit and one credit.';
COMMENT ON TABLE idempotency_records IS 'Durable client keys. A committed row always references its transfer.';

-- +goose Down

DROP TABLE IF EXISTS idempotency_records;
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
DROP TABLE IF EXISTS wallets;
DROP FUNCTION IF EXISTS idempotency_requires_transfer();
DROP FUNCTION IF EXISTS ledger_entries_trigger();
DROP FUNCTION IF EXISTS transfers_ledger_trigger();
DROP FUNCTION IF EXISTS check_transfer_ledger(uuid);
