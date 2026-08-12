-- Wallet transfer service — initial schema.
--
-- Design notes:
--   * Money is stored as BIGINT minor units (e.g. paise). Never floating point.
--   * ledger_entries is the append-only audit truth; wallets.balance_minor is the
--     lockable, fast-to-read projection of it. An invariant test asserts they agree.
--   * Only SYSTEM wallets may hold a negative balance; they represent the boundary
--     between this service and the outside world, which keeps SUM(balance_minor) = 0.
--
-- See docs/design.md for the full rationale.

CREATE TYPE transfer_status   AS ENUM ('PENDING', 'PROCESSED', 'FAILED');
CREATE TYPE ledger_entry_type AS ENUM ('DEBIT', 'CREDIT');
CREATE TYPE wallet_kind       AS ENUM ('USER', 'SYSTEM');

CREATE TABLE wallets (
    id            TEXT        PRIMARY KEY,
    kind          wallet_kind NOT NULL DEFAULT 'USER',
    currency      CHAR(3)     NOT NULL,
    balance_minor BIGINT      NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wallets_id_format       CHECK (id ~ '^[A-Za-z0-9_-]{1,64}$'),
    CONSTRAINT wallets_currency_format CHECK (currency ~ '^[A-Z]{3}$'),
    -- The double-spend backstop: application logic may only ever be the fast path.
    CONSTRAINT wallets_non_negative    CHECK (kind = 'SYSTEM' OR balance_minor >= 0)
);

CREATE TABLE transfers (
    id              UUID            PRIMARY KEY,
    idempotency_key TEXT            NOT NULL,
    request_hash    CHAR(64)        NOT NULL,   -- sha256 of the canonical request
    from_wallet_id  TEXT            NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    to_wallet_id    TEXT            NOT NULL REFERENCES wallets (id) ON DELETE RESTRICT,
    amount_minor    BIGINT          NOT NULL,
    currency        CHAR(3)         NOT NULL,
    status          transfer_status NOT NULL DEFAULT 'PENDING',
    failure_code    TEXT,
    failure_message TEXT,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT now(),
    settled_at      TIMESTAMPTZ,
    -- Exactly-once at the API level lives on this one line.
    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
    CONSTRAINT transfers_amount_positive  CHECK (amount_minor > 0 AND amount_minor <= 1000000000000000),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_shape    CHECK ((status = 'FAILED')    = (failure_code IS NOT NULL)),
    CONSTRAINT transfers_settled_shape    CHECK ((status = 'PROCESSED') = (settled_at   IS NOT NULL))
);

CREATE INDEX transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_created_idx   ON transfers (to_wallet_id,   created_at DESC);
-- Partial index: supports a future reconciliation sweeper over stuck PENDING rows
-- without carrying the cost of indexing every settled transfer.
CREATE INDEX transfers_pending_idx ON transfers (created_at) WHERE status = 'PENDING';

CREATE TABLE ledger_entries (
    id            BIGSERIAL         PRIMARY KEY,
    transfer_id   UUID              NOT NULL REFERENCES transfers (id) ON DELETE RESTRICT,
    wallet_id     TEXT              NOT NULL REFERENCES wallets   (id) ON DELETE RESTRICT,
    entry_type    ledger_entry_type NOT NULL,
    amount_minor  BIGINT            NOT NULL,
    currency      CHAR(3)           NOT NULL,
    balance_after BIGINT            NOT NULL,
    created_at    TIMESTAMPTZ       NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount_minor > 0),
    -- Guarantees at most one DEBIT and at most one CREDIT per transfer; combined with
    -- both rows being written in the same transaction as the PROCESSED transition,
    -- this yields exactly two balanced entries per processed transfer.
    CONSTRAINT ledger_entries_one_per_side UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id DESC);

-- ---------------------------------------------------------------------------
-- Integrity triggers
-- ---------------------------------------------------------------------------

-- Transfers: core fields are immutable, and the only legal transitions are
-- PENDING -> PROCESSED and PENDING -> FAILED.
CREATE FUNCTION enforce_transfer_invariants() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id              <> OLD.id
    OR NEW.idempotency_key <> OLD.idempotency_key
    OR NEW.request_hash    <> OLD.request_hash
    OR NEW.from_wallet_id  <> OLD.from_wallet_id
    OR NEW.to_wallet_id    <> OLD.to_wallet_id
    OR NEW.amount_minor    <> OLD.amount_minor
    OR NEW.currency        <> OLD.currency THEN
        RAISE EXCEPTION 'transfer % has immutable columns', OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.status <> OLD.status AND OLD.status <> 'PENDING' THEN
        RAISE EXCEPTION 'illegal transfer state transition % -> % on %',
            OLD.status, NEW.status, OLD.id
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER transfers_enforce_invariants
    BEFORE UPDATE ON transfers
    FOR EACH ROW EXECUTE FUNCTION enforce_transfer_invariants();

-- Ledger: append-only. A financial ledger that can be rewritten is not a ledger.
CREATE FUNCTION reject_ledger_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_entries is append-only (attempted %)', TG_OP
        USING ERRCODE = 'check_violation';
END;
$$;

CREATE TRIGGER ledger_entries_append_only
    BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION reject_ledger_mutation();
