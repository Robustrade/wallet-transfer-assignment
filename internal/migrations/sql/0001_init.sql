-- Wallet transfer service schema.
--
-- Design notes:
--   * All monetary amounts are stored as BIGINT in the smallest currency
--     unit (e.g. cents) to avoid floating point rounding errors.
--   * wallets.balance is a maintained (not derived) balance, updated inside
--     the same transaction that inserts ledger entries, so reads never need
--     to aggregate the ledger.
--   * transfers.idempotency_key is unique so the database itself is the
--     final backstop against duplicate transfers, even though the service
--     layer also performs an explicit idempotency claim step first.
--   * ledger_entries always references a transfer and can never be inserted
--     without one (FOREIGN KEY), keeping the ledger consistent with the
--     transfers table by construction.
--   * CHECK constraints enforce non-negative balances/amounts and a closed
--     set of enum-like string values at the database level, so invalid
--     states cannot be persisted even if application code has a bug.

CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT PRIMARY KEY,
    balance    BIGINT NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS transfers (
    id              TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets (id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    state           TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX IF NOT EXISTS idx_transfers_from_wallet ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_transfers_to_wallet ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          TEXT PRIMARY KEY,
    wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    transfer_id TEXT NOT NULL REFERENCES transfers (id),
    entry_type  TEXT NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet ON ledger_entries (wallet_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer ON ledger_entries (transfer_id);

-- One idempotency record per client-supplied key. request_hash detects
-- accidental reuse of the same key for a materially different request.
-- transfer_id / response_status / response_body / completed_at stay NULL
-- while the original request is still in flight, which is what lets a
-- concurrent duplicate distinguish "replay this response" from "the first
-- request hasn't finished yet".
CREATE TABLE IF NOT EXISTS idempotency_records (
    key             TEXT PRIMARY KEY,
    request_hash    TEXT NOT NULL,
    transfer_id     TEXT REFERENCES transfers (id),
    response_status INT,
    response_body   BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);
