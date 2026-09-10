-- +goose Up
CREATE TABLE IF NOT EXISTS transfers (
    id              VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(255) NOT NULL UNIQUE,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL CHECK (amount > 0),
    status          VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    error_reason    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT check_different_wallets CHECK (from_wallet_id <> to_wallet_id)
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    id          VARCHAR(64) PRIMARY KEY,
    transfer_id VARCHAR(64) NOT NULL REFERENCES transfers(id) ON DELETE RESTRICT,
    wallet_id   VARCHAR(64) NOT NULL REFERENCES wallets(id) ON DELETE RESTRICT,
    type        VARCHAR(10) NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
    amount      BIGINT NOT NULL CHECK (amount > 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_ledger_transfer_type UNIQUE (transfer_id, type)
);

CREATE INDEX IF NOT EXISTS idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX IF NOT EXISTS idx_ledger_wallet_id ON ledger_entries(wallet_id);

-- +goose Down
DROP TABLE IF EXISTS ledger_entries;
DROP TABLE IF EXISTS transfers;
