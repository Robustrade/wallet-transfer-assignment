CREATE TABLE wallets (
    id VARCHAR(100) PRIMARY KEY,

    balance BIGINT NOT NULL
        CHECK (balance >= 0),

    created_at TIMESTAMP NOT NULL
        DEFAULT NOW()
);


CREATE TABLE transfers (
    id UUID PRIMARY KEY,

    idempotency_key VARCHAR(255) NOT NULL UNIQUE,

    from_wallet_id VARCHAR(100) NOT NULL,

    to_wallet_id VARCHAR(100) NOT NULL,

    amount BIGINT NOT NULL
        CHECK (amount > 0),

    status VARCHAR(20) NOT NULL
        CHECK (
            status IN (
                'PENDING',
                'PROCESSED',
                'FAILED'
            )
        ),

    response JSONB,

    created_at TIMESTAMP NOT NULL
        DEFAULT NOW(),

    updated_at TIMESTAMP NOT NULL
        DEFAULT NOW(),

    CONSTRAINT different_wallets
        CHECK (from_wallet_id <> to_wallet_id)
);


CREATE TABLE ledger_entries (
    id BIGSERIAL PRIMARY KEY,

    wallet_id VARCHAR(100) NOT NULL,

    transfer_id UUID NOT NULL
        REFERENCES transfers(id),

    type VARCHAR(10) NOT NULL
        CHECK (
            type IN ('DEBIT', 'CREDIT')
        ),

    amount BIGINT NOT NULL
        CHECK (amount > 0),

    created_at TIMESTAMP NOT NULL
        DEFAULT NOW()
);


CREATE UNIQUE INDEX unique_transfer_debit
ON ledger_entries(transfer_id)
WHERE type = 'DEBIT';


CREATE UNIQUE INDEX unique_transfer_credit
ON ledger_entries(transfer_id)
WHERE type = 'CREDIT';