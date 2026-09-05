BEGIN;

CREATE TABLE IF NOT EXISTS wallets (
    id         TEXT        PRIMARY KEY,
    currency   TEXT        NOT NULL,
    balance    BIGINT      NOT NULL CHECK (balance >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallets_id_not_blank    CHECK (length(btrim(id)) > 0),
    CONSTRAINT wallets_currency_is_iso CHECK (currency ~ '^[A-Z]{3}$')
);

CREATE TABLE IF NOT EXISTS transfers (
    id             UUID        PRIMARY KEY,
    from_wallet_id TEXT        NOT NULL REFERENCES wallets (id),
    to_wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    amount         BIGINT      NOT NULL CHECK (amount > 0),
    currency       TEXT        NOT NULL,
    status         TEXT        NOT NULL,
    failure_reason TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_not_self     CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_failure_reason_present CHECK (
        (status = 'FAILED' AND failure_reason IS NOT NULL AND length(btrim(failure_reason)) > 0)
        OR (status <> 'FAILED' AND failure_reason IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS transfers_from_wallet_created_idx ON transfers (from_wallet_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS transfers_to_wallet_created_idx ON transfers (to_wallet_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          BIGSERIAL   PRIMARY KEY,
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    wallet_id   TEXT        NOT NULL REFERENCES wallets (id),
    entry_type  TEXT        NOT NULL,
    amount      BIGINT      NOT NULL CHECK (amount > 0),
    currency    TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ledger_entries_type_valid CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_one_per_side UNIQUE (transfer_id, entry_type)
);

CREATE INDEX IF NOT EXISTS ledger_entries_wallet_idx ON ledger_entries (wallet_id, id DESC);

CREATE TABLE IF NOT EXISTS idempotency_records (
    idempotency_key     TEXT        PRIMARY KEY,
    request_fingerprint TEXT        NOT NULL,
    transfer_id         UUID        NOT NULL REFERENCES transfers (id),
    response_body       JSONB       NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT idempotency_key_not_blank CHECK (length(btrim(idempotency_key)) > 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS idempotency_records_transfer_idx ON idempotency_records (transfer_id);

COMMIT;
