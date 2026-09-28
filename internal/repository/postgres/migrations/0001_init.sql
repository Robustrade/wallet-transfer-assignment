-- PostgreSQL schema, equivalent to the SQLite one.
-- Money is BIGINT minor units, ids are UUID, timestamps are TIMESTAMPTZ.

CREATE TABLE wallets (
    id         TEXT        NOT NULL PRIMARY KEY,
    balance    BIGINT      NOT NULL CHECK (balance >= 0),
    currency   TEXT        NOT NULL CHECK (char_length(currency) = 3),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE transfers (
    id              UUID        NOT NULL PRIMARY KEY,       -- UUIDv7
    idempotency_key TEXT        NOT NULL UNIQUE,            -- at most one transfer per key
    from_wallet_id  TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT        NOT NULL REFERENCES wallets (id),
    amount          BIGINT      NOT NULL CHECK (amount > 0),
    status          TEXT        NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  TEXT                 CHECK (failure_reason IN ('INSUFFICIENT_FUNDS', 'CURRENCY_MISMATCH', 'BALANCE_OVERFLOW')),
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    CHECK (from_wallet_id <> to_wallet_id),
    -- A reason is present exactly when the transfer FAILED.
    CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE INDEX idx_transfers_from_created ON transfers (from_wallet_id, created_at);
CREATE INDEX idx_transfers_to_created   ON transfers (to_wallet_id, created_at);

CREATE TABLE ledger_entries (
    id          UUID        NOT NULL PRIMARY KEY,           -- UUIDv7
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    entry_type  TEXT        NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL,
    -- At most one DEBIT and one CREDIT per transfer.
    UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_wallet_created ON ledger_entries (wallet_id, created_at);

CREATE TABLE idempotency_records (
    "key"        TEXT        NOT NULL PRIMARY KEY,
    request_hash TEXT        NOT NULL CHECK (char_length(request_hash) = 64),  -- sha256 hex
    -- NULL only inside the transaction that inserted the row.
    transfer_id  UUID        UNIQUE REFERENCES transfers (id),
    created_at   TIMESTAMPTZ NOT NULL
);
