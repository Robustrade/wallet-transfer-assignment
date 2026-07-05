-- Wallet balances. balance is a non-negative BIGINT (smallest currency
-- unit, e.g. cents) — the CHECK is a last line of defense; the application
-- never issues an UPDATE that could violate it because balance is checked
-- under a row lock before every debit.
CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL CHECK (balance >= 0),
    version     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per transfer attempt. idempotency_key is NOT unique here
-- (idempotency_records below is the source of truth for that) — it's just
-- carried along for traceability from a transfer back to the request that
-- created it.
CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    idempotency_key  TEXT NOT NULL,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets(id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    state            TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE INDEX idx_transfers_idempotency_key ON transfers (idempotency_key);
CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers (to_wallet_id);

-- Double-entry ledger. Every PROCESSED transfer writes exactly one DEBIT and
-- one CREDIT row here with equal amounts, so summing entries per wallet
-- always reconciles with wallets.balance.
CREATE TABLE ledger_entries (
    id           TEXT PRIMARY KEY,
    wallet_id    TEXT NOT NULL REFERENCES wallets(id),
    transfer_id  TEXT NOT NULL REFERENCES transfers(id),
    type         TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_wallet ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries (transfer_id);

-- The idempotency mechanism itself. The PRIMARY KEY on idempotency_key is
-- what makes concurrent duplicate requests safe: only one INSERT for a given
-- key can ever succeed, so every other concurrent attempt (or later retry)
-- hits a unique_violation and falls back to reading this row instead of
-- re-executing the transfer.
CREATE TABLE idempotency_records (
    idempotency_key  TEXT PRIMARY KEY,
    request_hash     TEXT NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED')),
    transfer_id      TEXT REFERENCES transfers(id),
    response_body    TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
