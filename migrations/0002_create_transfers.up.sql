CREATE TABLE transfers (
    id              BIGINT PRIMARY KEY,
    request_hash    VARCHAR(64) NOT NULL,
    from_wallet_id  VARCHAR(36) NOT NULL REFERENCES wallets (id),
    to_wallet_id    VARCHAR(36) NOT NULL REFERENCES wallets (id),
    amount          DECIMAL(18, 2) NOT NULL,
    status          VARCHAR(20) NOT NULL CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    failure_reason  VARCHAR(100),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
