-- Wallet transfer schema.
-- Amounts are BIGINT minor units (e.g. paise/cents). No floating point money.

CREATE TABLE wallets (
    id          TEXT        PRIMARY KEY,
    balance     BIGINT      NOT NULL CHECK (balance >= 0), -- last line of defence against overdraft
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    -- failure_reason is set if and only if the transfer FAILED
    CONSTRAINT transfers_failure_reason_matches_status
        CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE INDEX transfers_from_wallet_idx ON transfers (from_wallet_id, created_at);
CREATE INDEX transfers_to_wallet_idx   ON transfers (to_wallet_id, created_at);

CREATE TABLE ledger_entries (
    id           BIGSERIAL   PRIMARY KEY,
    transfer_id  UUID        NOT NULL REFERENCES transfers (id),
    wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    type         TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT      NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- at most one debit and one credit per transfer
    CONSTRAINT ledger_entries_one_per_type UNIQUE (transfer_id, type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);

CREATE TABLE idempotency_records (
    idempotency_key  TEXT        PRIMARY KEY,
    request_hash     TEXT        NOT NULL,
    transfer_id      UUID        NOT NULL UNIQUE REFERENCES transfers (id),
    -- snapshot of the final transfer, returned as-is on replay; NULL while PENDING
    result           JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Double-entry guard, checked at COMMIT time (deferred):
-- a transfer that has ledger rows must be PROCESSED and must have exactly one DEBIT on the
-- source wallet and one CREDIT on the destination wallet, both equal to the transfer amount.
CREATE FUNCTION check_ledger_balanced() RETURNS trigger AS $$
DECLARE
    t        transfers%ROWTYPE;
    n_debit  INTEGER;
    n_credit INTEGER;
BEGIN
    SELECT * INTO t FROM transfers WHERE id = NEW.transfer_id;

    SELECT
        count(*) FILTER (WHERE type = 'DEBIT'  AND wallet_id = t.from_wallet_id AND amount = t.amount),
        count(*) FILTER (WHERE type = 'CREDIT' AND wallet_id = t.to_wallet_id   AND amount = t.amount)
    INTO n_debit, n_credit
    FROM ledger_entries
    WHERE transfer_id = NEW.transfer_id;

    IF t.status <> 'PROCESSED' OR n_debit <> 1 OR n_credit <> 1 THEN
        RAISE EXCEPTION 'unbalanced ledger for transfer % (status=%, debits=%, credits=%)',
            NEW.transfer_id, t.status, n_debit, n_credit
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balanced
    AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION check_ledger_balanced();

-- Ledger is append-only.
CREATE FUNCTION reject_ledger_change() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only' USING ERRCODE = 'check_violation';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_change();
