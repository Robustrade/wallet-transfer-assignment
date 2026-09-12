CREATE TABLE wallets (
    id          TEXT PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0 CHECK (balance >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id               UUID PRIMARY KEY,
    -- Named explicitly: the repository matches on this constraint name to tell
    -- an idempotency clash apart from any other unique violation.
    idempotency_key  TEXT NOT NULL CONSTRAINT transfers_idempotency_key_key UNIQUE,
    request_hash     TEXT NOT NULL,
    from_wallet_id   TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id     TEXT NOT NULL REFERENCES wallets (id),
    amount           BIGINT NOT NULL CHECK (amount > 0),
    state            TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY,
    wallet_id    TEXT NOT NULL REFERENCES wallets (id),
    transfer_id  UUID NOT NULL REFERENCES transfers (id),
    type         TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount       BIGINT NOT NULL CHECK (amount > 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);
CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers (to_wallet_id);
