-- V2: Create transfers table
CREATE TABLE transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    idempotency_key TEXT NOT NULL,
    from_wallet_id  TEXT NOT NULL REFERENCES wallets (id),
    to_wallet_id    TEXT NOT NULL REFERENCES wallets (id),
    amount          NUMERIC(18, 4) NOT NULL CONSTRAINT transfers_amount_positive CHECK (amount > 0),
    status          TEXT NOT NULL DEFAULT 'PENDING'
                        CONSTRAINT transfers_status_valid CHECK (status IN ('PENDING', 'PROCESSED', 'FAILED')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT transfers_idempotency_key_unique UNIQUE (idempotency_key)
);

CREATE INDEX idx_transfers_from_wallet ON transfers (from_wallet_id);
CREATE INDEX idx_transfers_status      ON transfers (status);

