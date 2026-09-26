-- Wallet Transfer Service: initial schema.
--
-- Model: wallets hold a stored balance; every balance change is recorded as
-- exactly one double-entry pair (DEBIT + CREDIT) in ledger_entries, tied to a
-- transfer row. Idempotency is enforced by a primary key on
-- idempotency_records.idempotency_key, which makes duplicate API requests
-- impossible to commit twice (see README for the transaction design).
--
-- Amounts are integers in minor units (e.g. cents). No floats, ever.

CREATE TABLE wallets (
    id         text        PRIMARY KEY,
    balance    bigint      NOT NULL,
    currency   char(3)     NOT NULL DEFAULT 'USD',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT wallets_id_not_blank         CHECK (id <> ''),
    -- The ledger and the balance must never disagree on sign.
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    from_wallet_id text        NOT NULL REFERENCES wallets (id),
    to_wallet_id   text        NOT NULL REFERENCES wallets (id),
    amount         bigint      NOT NULL,
    status         text        NOT NULL DEFAULT 'PENDING',
    failure_reason text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT transfers_amount_positive  CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    -- State machine: PENDING -> PROCESSED | FAILED. Terminal states never change.
    CONSTRAINT transfers_status_valid     CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason_consistent
        CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL)),
    -- Redundant for uniqueness (id is the PK) but required as the target of
    -- the composite FK on ledger_entries, which pins each ledger entry to the
    -- transfer's own amount.
    CONSTRAINT transfers_id_amount_unique UNIQUE (id, amount)
);

CREATE TABLE ledger_entries (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transfer_id uuid        NOT NULL,
    wallet_id   text        NOT NULL REFERENCES wallets (id),
    entry_type  text        NOT NULL,
    amount      bigint      NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ledger_entry_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_entry_type_valid      CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    -- Composite FK: a ledger entry can only reference an existing transfer and
    -- must carry exactly that transfer's amount. Debit == credit is therefore
    -- structural, not a convention.
    CONSTRAINT ledger_transfer_amount_fk
        FOREIGN KEY (transfer_id, amount) REFERENCES transfers (id, amount),
    -- At most one DEBIT and one CREDIT per transfer: duplicate entries are
    -- rejected by the database. ("Exactly two per PROCESSED transfer" is
    -- covered by the transaction design and verified by tests.)
    CONSTRAINT ledger_transfer_entry_type_unique UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);
CREATE INDEX transfers_from_wallet_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_idx   ON transfers (to_wallet_id, created_at DESC);

-- Idempotency is claimed first (INSERT ... ON CONFLICT DO NOTHING) at the very
-- start of the transfer transaction, before any wallet is locked. The row is
-- "claimed" while response_status IS NULL and "complete" once the outcome has
-- been recorded; a complete row is replayed byte-for-byte by later duplicates.
CREATE TABLE idempotency_records (
    idempotency_key text        PRIMARY KEY,
    request_body    jsonb       NOT NULL,
    transfer_id     uuid        REFERENCES transfers (id),
    response_status smallint,
    response_body   text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT idem_response_pair  CHECK ((response_status IS NULL) = (response_body IS NULL)),
    -- Only outcomes worth replaying are recorded. 400 (malformed) and 409
    -- (key reuse with a different payload) never write an idempotency row.
    CONSTRAINT idem_response_status_known CHECK (response_status IS NULL OR response_status IN (201, 404, 422)),
    CONSTRAINT idem_transfer_matches_outcome
        CHECK (CASE
            WHEN response_status IN (201, 422) THEN transfer_id IS NOT NULL
            ELSE transfer_id IS NULL
        END)
);
