CREATE TYPE entry_type AS ENUM ('DEBIT', 'CREDIT');

CREATE TABLE wallets (
    id         BIGSERIAL PRIMARY KEY,
    balance    BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE transfers (
    id               TEXT PRIMARY KEY,
    idempotency_key  TEXT UNIQUE NOT NULL,
    from_wallet_id   BIGINT NOT NULL REFERENCES wallets(id),
    to_wallet_id     BIGINT NOT NULL REFERENCES wallets(id),
    amount           BIGINT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'PENDING',
    failure_reason   TEXT,
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);

CREATE TABLE ledger_entries (
    id          BIGSERIAL PRIMARY KEY,
    transfer_id TEXT NOT NULL REFERENCES transfers(id),
    wallet_id   BIGINT NOT NULL REFERENCES wallets(id),
    type        entry_type NOT NULL,
    amount      BIGINT NOT NULL,
    created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_ledger_wallet   ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_transfer ON ledger_entries(transfer_id);

CREATE TABLE idempotency_records (
    idempotency_key TEXT PRIMARY KEY,
    response_status INT NOT NULL,
    response_body   TEXT NOT NULL,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
