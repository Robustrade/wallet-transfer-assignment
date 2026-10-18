CREATE TABLE transfers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL UNIQUE,

    from_wallet_id BIGINT NOT NULL,
    to_wallet_id BIGINT NOT NULL,

    amount BIGINT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    failure_reason TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_transfers_from_wallet
        FOREIGN KEY (from_wallet_id) REFERENCES wallets(id),

    CONSTRAINT fk_transfers_to_wallet
        FOREIGN KEY (to_wallet_id) REFERENCES wallets(id),

    CONSTRAINT chk_transfer_amount
        CHECK (amount > 0),

    CONSTRAINT chk_different_wallets
        CHECK (from_wallet_id <> to_wallet_id),

    CONSTRAINT chk_transfer_status
        CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED'))
);

CREATE INDEX idx_transfers_from_wallet
    ON transfers(from_wallet_id);

CREATE INDEX idx_transfers_to_wallet
    ON transfers(to_wallet_id);