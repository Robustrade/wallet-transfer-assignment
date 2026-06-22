CREATE TABLE wallets (
    id UUID PRIMARY KEY,

    balance BIGINT NOT NULL
        CHECK (balance >= 0),

    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
CREATE TABLE transfers (
    id UUID PRIMARY KEY,

    from_wallet_id UUID NOT NULL,
    to_wallet_id UUID NOT NULL,

    amount BIGINT NOT NULL,

    status VARCHAR(20) NOT NULL,

    failure_reason VARCHAR(500),

    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,

    CONSTRAINT fk_transfer_from_wallet
        FOREIGN KEY (from_wallet_id)
        REFERENCES wallets(id),

    CONSTRAINT fk_transfer_to_wallet
        FOREIGN KEY (to_wallet_id)
        REFERENCES wallets(id)
);

CREATE TABLE ledger_entries (
    id UUID PRIMARY KEY,

    transfer_id UUID NOT NULL,
    wallet_id UUID NOT NULL,

    type VARCHAR(20) NOT NULL,

    amount BIGINT NOT NULL,

    created_at TIMESTAMP NOT NULL,

    CONSTRAINT fk_ledger_transfer
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CONSTRAINT fk_ledger_wallet
        FOREIGN KEY (wallet_id)
        REFERENCES wallets(id)
);

CREATE TABLE idempotency_records (
    id UUID PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL,

    request_hash VARCHAR(255) NOT NULL,

    transfer_id UUID,

    response_payload TEXT,

    status VARCHAR(20) NOT NULL,

    created_at TIMESTAMP NOT NULL,

    CONSTRAINT uk_idempotency_key
        UNIQUE (idempotency_key),

    CONSTRAINT fk_idempotency_transfer
        FOREIGN KEY (transfer_id)
        REFERENCES transfers(id)
);

CREATE INDEX idx_transfer_from_wallet
    ON transfers(from_wallet_id);

CREATE INDEX idx_transfer_to_wallet
    ON transfers(to_wallet_id);

CREATE INDEX idx_ledger_transfer
    ON ledger_entries(transfer_id);

CREATE INDEX idx_ledger_wallet
    ON ledger_entries(wallet_id);

CREATE INDEX idx_idempotency_request_hash
    ON idempotency_records(request_hash);