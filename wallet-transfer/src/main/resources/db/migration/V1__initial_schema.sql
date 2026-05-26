CREATE TABLE wallets (
    id         VARCHAR(50)   PRIMARY KEY,
    balance    NUMERIC(19,4) NOT NULL DEFAULT 0,
    version    BIGINT        NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id               UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key  VARCHAR(255)  NOT NULL,
    from_wallet_id   VARCHAR(50)   NOT NULL REFERENCES wallets(id),
    to_wallet_id     VARCHAR(50)   NOT NULL REFERENCES wallets(id),
    amount           NUMERIC(19,4) NOT NULL,
    status           VARCHAR(20)   NOT NULL DEFAULT 'PENDING',
    failure_reason   TEXT,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_amount_positive   CHECK (amount > 0),
    CONSTRAINT chk_different_wallets CHECK (from_wallet_id != to_wallet_id)
);

CREATE UNIQUE INDEX idx_transfers_idempotency_key ON transfers(idempotency_key);
CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet   ON transfers(to_wallet_id);
CREATE INDEX idx_transfers_status      ON transfers(status);

CREATE TABLE ledger_entries (
    id          UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id   VARCHAR(50)   NOT NULL REFERENCES wallets(id),
    transfer_id UUID          NOT NULL REFERENCES transfers(id),
    entry_type  VARCHAR(10)   NOT NULL,
    amount      NUMERIC(19,4) NOT NULL,
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_entry_type        CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT chk_ledger_amount_pos CHECK (amount > 0)
);

CREATE INDEX idx_ledger_transfer_id ON ledger_entries(transfer_id);
CREATE INDEX idx_ledger_wallet_id   ON ledger_entries(wallet_id);
