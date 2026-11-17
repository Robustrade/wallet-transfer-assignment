-- Wallet transfer schema (PostgreSQL 16).
-- Amounts are positive bigint minor units (implicit INR). No floats.
-- Idempotency key is claimed in the same transaction as the money movement.
-- Ledger: balance = opening_balance + credits - debits (enforced by the service).

-- Stored balance for concurrent locking; opening_balance for reconciliation.
CREATE TABLE wallets (
    id               text PRIMARY KEY CHECK (length(id) > 0 AND id = btrim(id)),
    opening_balance  bigint NOT NULL CHECK (opening_balance >= 0), -- set once at create; never updated
    balance          bigint NOT NULL CHECK (balance >= 0),         -- current funds; updated in transfer tx
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- One row per idempotency key. Claim first; store response when the outcome is known.
CREATE TABLE idempotency_records (
    idempotency_key  text PRIMARY KEY CHECK (length(idempotency_key) > 0 AND idempotency_key = btrim(idempotency_key)),
    request_hash     text NOT NULL,   -- sha256 of canonical request; replay vs mismatch
    request_body     jsonb NOT NULL,  -- canonical request payload
    response_code    integer,         -- null until outcome; then HTTP status (201/422)
    response_body    jsonb,           -- null until outcome; then response JSON (includes transferId)
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    -- response fields are both null (in-flight claim) or both set (terminal)
    CONSTRAINT idempotency_response_pair CHECK (
        (response_code IS NULL AND response_body IS NULL)
        OR (response_code IS NOT NULL AND response_body IS NOT NULL)
    )
);

-- One transfer per idempotency key. Insert PENDING, then PROCESSED or FAILED before commit.
CREATE TABLE transfers (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  text NOT NULL UNIQUE
                        REFERENCES idempotency_records (idempotency_key), -- 1:1 with claim
    from_wallet_id   text NOT NULL REFERENCES wallets (id),
    to_wallet_id     text NOT NULL REFERENCES wallets (id),
    amount           bigint NOT NULL CHECK (amount > 0),
    status           text NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   text,            -- required when FAILED; null otherwise
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_reason CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL AND length(failure_reason) > 0)
        OR (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

-- Double-entry: one DEBIT and one CREDIT per PROCESSED transfer. None for FAILED.
CREATE TABLE ledger_entries (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id      text NOT NULL REFERENCES wallets (id),
    transfer_id    uuid NOT NULL REFERENCES transfers (id),
    entry_type     text NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount         bigint NOT NULL CHECK (amount > 0), -- always positive; direction is entry_type
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, entry_type) -- at most one debit and one credit per transfer
);

CREATE INDEX transfers_from_wallet_id_idx ON transfers (from_wallet_id);
CREATE INDEX transfers_to_wallet_id_idx ON transfers (to_wallet_id);
CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id);

-- opening_balance is the immutable baseline for reconciliation; reject any change.
CREATE FUNCTION wallets_opening_balance_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.opening_balance IS DISTINCT FROM OLD.opening_balance THEN
        RAISE EXCEPTION 'opening_balance is immutable'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER wallets_opening_balance_immutable
    BEFORE UPDATE ON wallets
    FOR EACH ROW EXECUTE FUNCTION wallets_opening_balance_immutable();

-- At commit: balance must equal opening_balance + credits - debits for the wallet.
CREATE FUNCTION assert_wallet_balance_equals_ledger(wid text) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    opening bigint;
    bal     bigint;
    net     bigint;
BEGIN
    SELECT opening_balance, balance
      INTO opening, bal
      FROM wallets
     WHERE id = wid;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'wallet % is missing during balance check', wid
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(
               CASE entry_type WHEN 'CREDIT' THEN amount ELSE -amount END
           ), 0)
      INTO net
      FROM ledger_entries
     WHERE wallet_id = wid;

    IF bal IS DISTINCT FROM opening + net THEN
        RAISE EXCEPTION
            'wallet % balance % does not equal opening_balance % plus ledger net %',
            wid, bal, opening, net
            USING ERRCODE = '23514';
    END IF;
END;
$$;

CREATE FUNCTION wallets_balance_must_match() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_wallet_balance_equals_ledger(NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION ledger_entries_balance_must_match() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM assert_wallet_balance_equals_ledger(NEW.wallet_id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER wallets_balance_match
    AFTER INSERT OR UPDATE OF balance ON wallets
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION wallets_balance_must_match();

CREATE CONSTRAINT TRIGGER ledger_entries_balance_match
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION ledger_entries_balance_must_match();
