CREATE TABLE wallets (
    id BIGSERIAL PRIMARY KEY,

    wallet_id VARCHAR(100) NOT NULL UNIQUE,
    balance float NOT NULL DEFAULT 0,
    -- balance NUMERIC(18,2) NOT NULL DEFAULT 0,

    version BIGINT NOT NULL DEFAULT 0,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE transfers (
    id BIGSERIAL PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL UNIQUE,

    from_wallet_id VARCHAR(100) NOT NULL,

    to_wallet_id VARCHAR(100) NOT NULL,

    amount float NOT NULL,

    state VARCHAR(20) NOT NULL,

    failure_reason TEXT,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    FOREIGN KEY (from_wallet_id)
        REFERENCES wallets(wallet_id),

    FOREIGN KEY (to_wallet_id)
        REFERENCES wallets(wallet_id),

    CHECK (amount > 0),

    CHECK (
        state IN (
            'PENDING',
            'PROCESSED',
            'FAILED'
        )
    )
);

CREATE INDEX idx_transfer_from
ON transfers(from_wallet_id);

CREATE INDEX idx_transfer_to
ON transfers(to_wallet_id);


CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,

    wallet_id VARCHAR(100) NOT NULL,

    transfer_id BIGINT NOT NULL,

    entry_type VARCHAR(20) NOT NULL,

    amount float NOT NULL,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    FOREIGN KEY (wallet_id)
        REFERENCES wallets(wallet_id),

    FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CHECK (
        entry_type IN (
            'DEBIT',
            'CREDIT'
        )
    )
);

CREATE INDEX idx_ledger_wallet
ON ledger_entries(wallet_id);

CREATE INDEX idx_ledger_transfer
ON ledger_entries(transfer_id);


CREATE TABLE idempotency_records (
    id BIGSERIAL PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL UNIQUE,

    request_hash TEXT, 
    -- can consider hash not null

    transfer_id BIGINT,

    response JSONB,

    status VARCHAR(20) NOT NULL,

    created_at TIMESTAMP NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),

    FOREIGN KEY (transfer_id)
        REFERENCES transfers(id),

    CHECK (
        status IN (
            'IN_PROGRESS',
            'COMPLETED',
            'FAILED'
        )
    )
);

ALTER TABLE transfers
ADD CONSTRAINT chk_different_wallets
CHECK (
    from_wallet_id <> to_wallet_id
);




INSERT INTO wallets (
    wallet_id,
    balance,
    version
)
VALUES
    ('wallet_1', 10000.00, 0),
    ('wallet_2', 5000.00, 0),
    ('wallet_3', 2500.00, 0),
    ('wallet_4', 7500.00, 0),
    ('wallet_5', 12000.00, 0);

