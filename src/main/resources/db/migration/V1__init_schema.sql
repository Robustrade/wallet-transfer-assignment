CREATE TABLE wallets (
    id          VARCHAR(64) PRIMARY KEY,
    balance     BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_wallet_balance_non_negative CHECK (balance >= 0)
);

CREATE TABLE transfers (
    id              VARCHAR(64) PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL,
    from_wallet_id  VARCHAR(64) NOT NULL REFERENCES wallets(id),
    to_wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    amount          BIGINT NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'PENDING',
    failure_balance BIGINT,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_idempotency_key UNIQUE (idempotency_key),
    CONSTRAINT chk_transfer_amount_positive CHECK (amount > 0),
    CONSTRAINT chk_transfer_different_wallets CHECK (from_wallet_id <> to_wallet_id),
    CONSTRAINT chk_transfer_status CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE TABLE ledger_entries (
    id           BIGSERIAL PRIMARY KEY,
    wallet_id    VARCHAR(64) NOT NULL REFERENCES wallets(id),
    transfer_id  VARCHAR(64) NOT NULL REFERENCES transfers(id),
    entry_type   VARCHAR(8) NOT NULL,
    amount       BIGINT NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_entry_type CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    CONSTRAINT chk_entry_amount_positive CHECK (amount > 0),
    CONSTRAINT uq_ledger_transfer_wallet_type UNIQUE (transfer_id, wallet_id, entry_type)
);

CREATE INDEX idx_transfers_from_wallet ON transfers(from_wallet_id);
CREATE INDEX idx_transfers_to_wallet ON transfers(to_wallet_id);
CREATE INDEX idx_transfers_status ON transfers(status);
CREATE INDEX idx_ledger_entries_wallet ON ledger_entries(wallet_id);
CREATE INDEX idx_ledger_entries_transfer ON ledger_entries(transfer_id);
