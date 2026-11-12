BEGIN;

CREATE TABLE wallets (
    id          TEXT        PRIMARY KEY,
    balance     BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT wallets_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE idempotency_records (
    key            TEXT        PRIMARY KEY,
    request_hash   TEXT        NOT NULL,
    response_code  INT,
    response_body  JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT idempotency_records_response_complete
        CHECK ((response_code IS NULL) = (response_body IS NULL))
);

CREATE TABLE transfers (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  TEXT        NOT NULL,
    from_wallet_id   TEXT        NOT NULL,
    to_wallet_id     TEXT        NOT NULL,
    amount           BIGINT      NOT NULL,
    status           TEXT        NOT NULL,
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key),
    CONSTRAINT transfers_idempotency_key_fk
        FOREIGN KEY (idempotency_key) REFERENCES idempotency_records (key),
    CONSTRAINT transfers_from_wallet_fk
        FOREIGN KEY (from_wallet_id) REFERENCES wallets (id),
    CONSTRAINT transfers_to_wallet_fk
        FOREIGN KEY (to_wallet_id) REFERENCES wallets (id),
    CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT transfers_status_valid
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    CONSTRAINT transfers_failure_reason_only_when_failed
        CHECK ((status = 'FAILED') = (failure_reason IS NOT NULL))
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL   PRIMARY KEY,
    transfer_id  UUID        NOT NULL,
    wallet_id    TEXT        NOT NULL,
    entry_type   TEXT        NOT NULL,
    amount       BIGINT      NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT ledger_entries_transfer_fk
        FOREIGN KEY (transfer_id) REFERENCES transfers (id),
    CONSTRAINT ledger_entries_wallet_fk
        FOREIGN KEY (wallet_id) REFERENCES wallets (id),
    CONSTRAINT ledger_entries_entry_type_valid
        CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT ledger_entries_amount_positive CHECK (amount > 0),
    CONSTRAINT ledger_entries_one_per_type UNIQUE (transfer_id, entry_type)
);

CREATE INDEX idx_ledger_entries_wallet_created ON ledger_entries (wallet_id, created_at);
CREATE INDEX idx_transfers_from_wallet_created ON transfers (from_wallet_id, created_at);
CREATE INDEX idx_transfers_to_wallet_created   ON transfers (to_wallet_id, created_at);

COMMIT;
