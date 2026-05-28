BEGIN;

CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    -- balance >= 0: defence-in-depth. The service layer also checks this
    -- under a row lock before debiting, but the constraint catches any
    -- future code path that forgets to lock + verify.
    balance     BIGINT      NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              UUID        PRIMARY KEY,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    -- state is held as TEXT + CHECK rather than a Postgres ENUM because
    -- TEXT enums are trivial to evolve (ALTER TABLE) and friendlier to
    -- migrations than native ENUM types, which need DROP CASCADE games.
    state           TEXT        NOT NULL CHECK (state IN ('PENDING','PROCESSED','FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Self-transfers would inflate the ledger (one DEBIT and one CREDIT
    -- against the same wallet for the same transfer) without any net
    -- balance change. Forbidden at the schema level so no code path can
    -- accidentally produce one.
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX transfers_from_wallet_idx ON transfers(from_wallet_id);
CREATE INDEX transfers_to_wallet_idx   ON transfers(to_wallet_id);

CREATE TABLE ledger_entries (
    id           BIGSERIAL   PRIMARY KEY,
    transfer_id  UUID        NOT NULL REFERENCES transfers(id),
    wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    type         TEXT        NOT NULL CHECK (type IN ('DEBIT','CREDIT')),
    amount       BIGINT      NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- UNIQUE (transfer_id, type) guarantees exactly two rows per transfer
    -- (one DEBIT, one CREDIT) at the schema level, independent of what
    -- the service layer does. If the application accidentally inserts a
    -- second DEBIT/CREDIT for the same transfer it will be rejected with
    -- SQLSTATE 23505 instead of producing a silently unbalanced ledger.
    UNIQUE (transfer_id, type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries(wallet_id);

CREATE TABLE idempotency_records (
    -- key is the PRIMARY KEY so concurrent first-time inserts serialise
    -- on the unique-index lock: the loser blocks until the winner
    -- commits, then receives unique_violation (23505) which the service
    -- layer maps to ErrIdempotencyExists and replays the original
    -- response from the now-visible row.
    key           TEXT        PRIMARY KEY,
    request_hash  TEXT        NOT NULL,
    transfer_id   UUID        NOT NULL REFERENCES transfers(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMIT;
