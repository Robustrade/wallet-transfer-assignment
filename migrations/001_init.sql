-- Wallet Transfer Service — initial schema
--
-- Design notes:
--   * Amounts are BIGINT minor units (e.g. cents), never floating point.
--   * wallets.balance has a CHECK to make "double spending" impossible even
--     if application logic has a bug: the database itself refuses a
--     negative balance.
--   * transfers.idempotency_key is UNIQUE — this is the ultimate backstop
--     for exactly-once semantics: even if application-level locking had a
--     bug, two rows with the same key can never both exist.
--   * ledger_entries has a UNIQUE (transfer_id, wallet_id, type) constraint
--     so a given transfer can never accumulate more than one debit or one
--     credit leg, guarding the double-entry invariant at the schema level.
--   * idempotency_records is intentionally decoupled from transfers

CREATE TABLE IF NOT EXISTS wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id               UUID PRIMARY KEY,
    idempotency_key  TEXT NOT NULL UNIQUE,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets (id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    status           TEXT NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet   ON transfers (to_wallet_id);
CREATE INDEX IF NOT EXISTS idx_transfers_status      ON transfers (status);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id           UUID PRIMARY KEY,
    transfer_id  UUID NOT NULL REFERENCES transfers (id),
    wallet_id    TEXT NOT NULL REFERENCES wallets (id),
    type         TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (transfer_id, wallet_id, type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries (transfer_id);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key      TEXT PRIMARY KEY,
    request_fingerprint  TEXT NOT NULL,
    status               TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED')),
    transfer_id          UUID REFERENCES transfers (id),
    response_status      INT,
    response_body        JSONB,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
