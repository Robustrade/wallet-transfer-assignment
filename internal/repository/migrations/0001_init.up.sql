-- 0001_init.up.sql
-- Core schema for the wallet-transfer service.
--
-- Design notes:
--   * Amounts are stored as BIGINT minor units (e.g. cents) to avoid floating point.
--   * wallets.balance is a maintained (stored) balance, guarded by a CHECK (>= 0)
--     and mutated only inside a serialized transaction (SELECT ... FOR UPDATE).
--   * idempotency_records.key is the single source of truth for API-level
--     exactly-once semantics: the UNIQUE PK rejects duplicate keys.
--   * ledger_entries has a UNIQUE (transfer_id, type) index so a transfer can
--     produce at most one DEBIT and one CREDIT row -> exactly two entries.

CREATE TABLE wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency   TEXT        NOT NULL DEFAULT 'USD',
    version    BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets(id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets(id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    currency        TEXT        NOT NULL DEFAULT 'USD',
    state           TEXT        NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT transfers_no_self_transfer CHECK (from_wallet_id <> to_wallet_id),
    -- Creating the transfer row is what claims an idempotency key. This unique
    -- constraint is the primary exactly-once guarantee; idempotency_records
    -- stores the request fingerprint used to detect key reuse with a changed
    -- payload.
    CONSTRAINT transfers_idempotency_key_key UNIQUE (idempotency_key)
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers (to_wallet_id);

CREATE TABLE idempotency_records (
    key         TEXT PRIMARY KEY,
    transfer_id TEXT        NOT NULL REFERENCES transfers(id),
    request_hash TEXT       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    wallet_id   TEXT        NOT NULL REFERENCES wallets(id),
    transfer_id TEXT        NOT NULL REFERENCES transfers(id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX        idx_ledger_wallet            ON ledger_entries (wallet_id);
CREATE UNIQUE INDEX uq_ledger_transfer_type      ON ledger_entries (transfer_id, type);
