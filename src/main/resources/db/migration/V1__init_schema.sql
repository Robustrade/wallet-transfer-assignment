CREATE TABLE wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     NUMERIC(19, 2) NOT NULL,
    version     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transfers (
    id               UUID PRIMARY KEY,
    idempotency_key  VARCHAR(255) NOT NULL,
    from_wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets (id),
    to_wallet_id     VARCHAR(64) NOT NULL REFERENCES wallets (id),
    amount           NUMERIC(19, 2) NOT NULL,
    status           VARCHAR(16) NOT NULL,
    failure_reason   VARCHAR(255),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_transfers_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_transfers_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_transfers_distinct_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfers_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE INDEX idx_transfers_from_wallet_id ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_to_wallet_id ON transfers (to_wallet_id);

CREATE TABLE ledger_entries (
    id           UUID PRIMARY KEY,
    transfer_id  UUID NOT NULL REFERENCES transfers (id),
    wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets (id),
    entry_type   VARCHAR(8) NOT NULL,
    amount       NUMERIC(19, 2) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_ledger_entries_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_ledger_entries_type CHECK (entry_type IN ('DEBIT', 'CREDIT'))
);

CREATE INDEX idx_ledger_entries_wallet_id ON ledger_entries (wallet_id);
CREATE INDEX idx_ledger_entries_transfer_id ON ledger_entries (transfer_id);

CREATE TABLE idempotency_records (
    idempotency_key       VARCHAR(255) PRIMARY KEY,
    transfer_id           UUID REFERENCES transfers (id),
    response_status       INTEGER NOT NULL,
    response_body         TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
