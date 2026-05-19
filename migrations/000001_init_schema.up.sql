CREATE TABLE users (
    user_id    TEXT        PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE wallets (
    wallet_id  TEXT        PRIMARY KEY,
    user_id    TEXT        NOT NULL REFERENCES users (user_id),
    balance    BIGINT      NOT NULL DEFAULT 0 CHECK (balance >= 0),
    currency   VARCHAR(3)  NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    transfer_id       TEXT        PRIMARY KEY,
    source_wallet_id  TEXT        NOT NULL REFERENCES wallets (wallet_id),
    target_wallet_id  TEXT        NOT NULL REFERENCES wallets (wallet_id),
    amount            BIGINT      NOT NULL CHECK (amount > 0),
    currency          VARCHAR(3)  NOT NULL,
    state             TEXT        NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (source_wallet_id <> target_wallet_id)
);

CREATE INDEX idx_transfers_source_target
    ON transfers (source_wallet_id, target_wallet_id);

CREATE TABLE ledger_entries (
    entry_id    BIGSERIAL   NOT NULL,
    wallet_id   TEXT        NOT NULL REFERENCES wallets (wallet_id),
    transfer_id TEXT        NOT NULL REFERENCES transfers (transfer_id),
    type        TEXT        NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (wallet_id, transfer_id)
);

CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);

CREATE TABLE idempotency_records (
    idempotency_key  TEXT        PRIMARY KEY,
    transfer_id      TEXT        REFERENCES transfers (transfer_id),
    request_hash     TEXT        NOT NULL,
    response_status  INTEGER     NOT NULL,
    response_body    TEXT        NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
