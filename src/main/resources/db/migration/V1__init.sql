-- Wallet Transfer Service - initial schema
--
-- Design notes:
--  * All monetary amounts are NUMERIC(19,4) to avoid floating point error.
--  * wallets.balance is a maintained (not derived) balance, updated inside
--    the same transaction as the ledger entries that justify the change.
--    This keeps reads (GET balance) cheap and O(1) instead of requiring a
--    SUM() over the ledger on every read. The ledger remains the source of
--    truth for *how* the balance got there, and a reconciliation job could
--    recompute balances from the ledger at any time to detect drift.
--  * transfers.idempotency_key has a UNIQUE constraint: this is the
--    database-level backstop for exactly-once transfer creation, even if
--    two requests race past the application-level check.
--  * ledger_entries has no UPDATE path at all in the application - it is
--    append-only, which is what makes it trustworthy as a ledger.

CREATE TABLE wallets (
    id          VARCHAR(64)     PRIMARY KEY,
    balance     NUMERIC(19,4)   NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id               UUID            PRIMARY KEY,
    idempotency_key  VARCHAR(255)    NOT NULL,
    from_wallet_id   VARCHAR(64)     NOT NULL REFERENCES wallets(id),
    to_wallet_id     VARCHAR(64)     NOT NULL REFERENCES wallets(id),
    amount           NUMERIC(19,4)   NOT NULL,
    status           VARCHAR(16)     NOT NULL,
    failure_reason   VARCHAR(512),
    created_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfers_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);

CREATE TABLE ledger_entries (
    id           UUID            PRIMARY KEY,
    transfer_id  UUID            NOT NULL REFERENCES transfers(id),
    wallet_id    VARCHAR(64)     NOT NULL REFERENCES wallets(id),
    entry_type   VARCHAR(8)      NOT NULL,
    amount       NUMERIC(19,4)   NOT NULL,
    created_at   TIMESTAMPTZ     NOT NULL DEFAULT now(),
    CONSTRAINT chk_ledger_entry_type CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT chk_ledger_amount_positive CHECK (amount > 0)
);

CREATE INDEX idx_ledger_entries_wallet_id   ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);

-- A transfer must produce exactly two ledger entries (one DEBIT, one
-- CREDIT) with equal amounts. That invariant is enforced in the service
-- layer inside the same transaction that inserts both rows; it cannot be
-- expressed as a single-row CHECK constraint in standard Postgres, so it is
-- covered by tests (see LedgerCorrectnessTest) instead.

CREATE TABLE idempotency_records (
    idempotency_key  VARCHAR(255)   PRIMARY KEY,
    request_hash     VARCHAR(128)   NOT NULL,
    transfer_id      UUID           REFERENCES transfers(id),
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Seed a couple of wallets for local manual testing / curl-ing the API.
INSERT INTO wallets (id, balance) VALUES
    ('wallet_1', 1000.0000),
    ('wallet_2', 500.0000),
    ('wallet_3', 0.0000);
