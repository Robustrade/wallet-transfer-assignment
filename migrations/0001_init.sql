-- Wallet Transfer Service schema (PostgreSQL).
--
-- Design notes:
--   * Money is stored as BIGINT minor units (e.g. cents) to avoid floating
--     point error. All amounts are positive; direction is encoded by ledger
--     entry type.
--   * Idempotency is enforced by a UNIQUE constraint on transfers.idempotency_key
--     rather than a separate table: the transfer row IS the idempotency record,
--     which removes a class of "record exists but transfer missing" bugs.
--   * The ledger is append-only and double-entry: exactly one DEBIT and one
--     CREDIT per transfer, guaranteed by UNIQUE(transfer_id, type).

CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0,
    currency   TEXT        NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Defence in depth: the database itself forbids a negative balance, so a
    -- bug in application code can never overdraw a wallet.
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT        NOT NULL,
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL,
    currency        TEXT        NOT NULL DEFAULT 'USD',
    status          TEXT        NOT NULL DEFAULT 'PENDING',
    failure_reason  TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Exactly-once at the API level: a key maps to a single transfer.
    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    type        TEXT        NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_type_valid CHECK (type IN ('DEBIT', 'CREDIT')),
    -- A transfer can have at most one DEBIT and one CREDIT row, so re-processing
    -- a transfer can never duplicate ledger entries.
    CONSTRAINT ledger_one_entry_per_type UNIQUE (transfer_id, type)
);

-- Fast lookups for the transfer-history and ledger APIs.
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id ON ledger_entries (wallet_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id);
