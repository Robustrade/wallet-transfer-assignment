-- Wallet transfer schema.
-- Money is stored as BIGINT minor units (e.g. paise/cents) to avoid floating point errors.

CREATE TABLE wallets (
    id         VARCHAR(64) PRIMARY KEY,
    balance    BIGINT      NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Last line of defence against overdrafts, even if application logic is buggy.
    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id               UUID PRIMARY KEY,
    -- UNIQUE is the idempotency guarantee: one key can only ever map to one transfer.
    idempotency_key  VARCHAR(128) NOT NULL,
    -- SHA-256 of the business payload; detects the same key being reused for a different request.
    request_hash     CHAR(64)     NOT NULL,
    from_wallet_id   VARCHAR(64)  NOT NULL REFERENCES wallets (id),
    to_wallet_id     VARCHAR(64)  NOT NULL REFERENCES wallets (id),
    amount           BIGINT       NOT NULL,
    status           VARCHAR(16)  NOT NULL,
    failure_reason   VARCHAR(255),
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT transfers_idempotency_key_uk UNIQUE (idempotency_key),
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason_only_when_failed
        CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

-- Supports transfer history per wallet (either side), newest first.
CREATE INDEX transfers_from_wallet_idx ON transfers (from_wallet_id, created_at DESC);
CREATE INDEX transfers_to_wallet_idx ON transfers (to_wallet_id, created_at DESC);

CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets (id),
    transfer_id UUID        NOT NULL REFERENCES transfers (id),
    entry_type  VARCHAR(8)  NOT NULL,
    amount      BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ledger_entry_type_valid CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_amount_positive CHECK (amount > 0),
    -- A transfer can have at most one DEBIT and one CREDIT => exactly two entries once processed.
    CONSTRAINT ledger_one_entry_per_type_per_transfer UNIQUE (transfer_id, entry_type)
);

CREATE INDEX ledger_entries_wallet_idx ON ledger_entries (wallet_id, id);
