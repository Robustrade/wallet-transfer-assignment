-- This schema intentionally supports a single-currency, transfer-only service.
BEGIN;
-- BIGINT amounts represent minor units (not floating-point currency).

CREATE TABLE wallets (
    id TEXT PRIMARY KEY,
    opening_balance BIGINT NOT NULL CHECK (opening_balance >= 0),
    balance BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_id_format CHECK (
        length(id) BETWEEN 1 AND 128 AND id = btrim(id)
    )
);

-- Protect the immutable reconciliation baseline, including from accidental SQL.
CREATE FUNCTION reject_opening_balance_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.opening_balance IS DISTINCT FROM OLD.opening_balance THEN
        RAISE EXCEPTION 'wallet opening_balance is immutable';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER wallets_opening_balance_immutable
BEFORE UPDATE ON wallets
FOR EACH ROW EXECUTE FUNCTION reject_opening_balance_change();

CREATE TABLE transfers (
    id UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    to_wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    amount BIGINT NOT NULL CHECK (amount > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_different_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_idempotency_key_format CHECK (
        length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key = btrim(idempotency_key)
    ),
    CONSTRAINT transfers_failure_reason_consistency CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL AND length(btrim(failure_reason)) > 0)
        OR (status IN ('PENDING', 'PROCESSED') AND failure_reason IS NULL)
    )
);

CREATE TABLE ledger_entries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id UUID NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id TEXT NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_one_entry_per_type UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_id_idx ON ledger_entries (wallet_id);

-- Posted ledger entries are append-only: corrections would require new entries,
-- which are outside this assignment's transfer-only scope.
CREATE FUNCTION reject_ledger_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger entries are immutable';
END;
$$;

CREATE TRIGGER ledger_entries_immutable
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
COMMIT;
